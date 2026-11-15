package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/etkecc/go-apm/v2"
	echobasicauth "github.com/etkecc/go-echo-basic-auth/v2"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/etkecc/docker-registry-proxy/internal/config"
	drperrors "github.com/etkecc/docker-registry-proxy/internal/errors"
	"github.com/etkecc/docker-registry-proxy/internal/metrics"
	"github.com/etkecc/docker-registry-proxy/internal/utils"
)

var httpTransport http.RoundTripper

type middlewareService interface {
	Middleware() echo.MiddlewareFunc
}

type authService interface {
	middlewareService
	LoginVia(context.Context, string, string, string) error
}

type healthchecksService interface {
	Fail(optionalBody ...io.Reader)
}

// proxyWriter routes informational responses to the raw writer, so only the final status commits Echo's response.
type proxyWriter struct {
	http.ResponseWriter
	raw http.ResponseWriter
}

func (w *proxyWriter) WriteHeader(code int) {
	if code >= 100 && code <= 199 {
		w.raw.WriteHeader(code)
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

// CloseNotify lets the reverse proxy cancel the upstream call when the client goes away.
func (w *proxyWriter) CloseNotify() <-chan bool {
	if notifier, ok := w.raw.(interface{ CloseNotify() <-chan bool }); ok {
		return notifier.CloseNotify()
	}
	return make(chan bool)
}

// ConfigureRouter configures echo router
func ConfigureRouter(e *echo.Echo, metricsAuth *echobasicauth.Auth, authSvc authService, cacheSvc middlewareService, hcSvc healthchecksService, target config.Target) error {
	if err := configureTransport(); err != nil {
		return err
	}
	e.HTTPErrorHandler = httpErrorHandler
	e.Use(middleware.Recover())
	e.Use(middleware.Secure())
	e.Use(apm.WithSentry())
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Response().Header().Set(echo.HeaderReferrerPolicy, "origin")
			return next(c)
		}
	})
	e.IPExtractor = echo.ExtractIPFromXFFHeader(
		echo.TrustLoopback(true),
		echo.TrustLinkLocal(true),
		echo.TrustPrivateNet(true),
	)
	metricsAuthMiddleware := echobasicauth.NewMiddleware(metricsAuth)
	e.GET("/_health", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	e.GET("/_via", loginVia(authSvc), middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(1)))
	e.GET("/metrics", metrics.Handler(), metricsAuthMiddleware)

	e.Any("*", proxy(target, hcSvc), authSvc.Middleware(), cacheSvc.Middleware())

	return nil
}

// httpErrorHandler reports recovered panics, then writes Echo's default error response.
func httpErrorHandler(c *echo.Context, err error) {
	if panicErr, isPanic := errors.AsType[*middleware.PanicStackError](err); isPanic {
		c.Logger().Error("request panicked",
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
			"error", panicErr,
		)
	}
	echo.DefaultHTTPErrorHandler(false)(c, err)
}

// configureTransport configures the default HTTP transport with a custom dialer and APM instrumentation.
func configureTransport() error {
	dialer := &net.Dialer{
		Timeout:   10 * time.Minute,
		KeepAlive: 10 * time.Minute,
	}
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return errors.New("http.DefaultTransport is not an *http.Transport")
	}
	transport := defaultTransport.Clone()
	transport.DialContext = dialer.DialContext
	httpTransport = apm.WrapRoundTripper(transport, apm.WithMaxRetries(0))

	return nil
}

func loginVia(authSvc authService) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		ip := c.RealIP()
		domain := c.QueryParam("domain")
		via := c.QueryParam("via")
		if err := authSvc.LoginVia(ctx, ip, domain, via); err != nil {
			log := utils.NewLog(c)
			log.Warn("login via failed", "error", err)
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func proxy(target config.Target, hcSvc healthchecksService) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx := c.Request().Context()
		ctx = context.WithoutCancel(ctx)
		ctx = apm.NewContext(ctx)
		c.SetRequest(c.Request().WithContext(ctx))

		src := *c.Request().URL
		src.Host = c.Request().Host
		log := utils.NewLog(c)
		c.Request().Host = target.Host

		proxy := httputil.NewSingleHostReverseProxy(&url.URL{Host: target.Host, Scheme: target.Scheme})
		proxy.Transport = httpTransport
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { proxyError(w, r, hcSvc, err) }
		proxy.ModifyResponse = func(r *http.Response) error {
			// rewrite location header if needed
			if location := r.Header.Get("Location"); location != "" {
				locationURL, lerr := url.Parse(location)
				if lerr == nil && locationURL.Host == target.Host {
					locationURL.Host = src.Host
					r.Header.Set("Location", locationURL.String())
				}
			}
			log.Info("proxied", "resp.status", r.StatusCode, "req.url", r.Request.URL.String())
			return nil
		}

		resp, uerr := echo.UnwrapResponse(c.Response())
		if uerr != nil {
			return uerr
		}
		rw := &proxyWriter{ResponseWriter: resp, raw: resp.ResponseWriter}
		defer proxyRecover(ctx, rw, hcSvc)
		proxy.ServeHTTP(rw, c.Request())
		return nil
	}
}

func proxyError(w http.ResponseWriter, r *http.Request, hcSvc healthchecksService, err error) {
	var ctx context.Context
	var log *slog.Logger
	if r != nil {
		ctx = r.Context()
		log = apm.Log(ctx).With("method", r.Method, "url", r.URL.String())
	} else {
		ctx = apm.NewContext()
		log = apm.Log(ctx)
	}

	defer proxyRecover(ctx, w, hcSvc)
	log.Warn("failed", "error", err)
	if hcSvc != nil {
		hcSvc.Fail(strings.NewReader(fmt.Sprintf("%s %s failed: %+v", r.Method, r.URL.String(), err)))
	}
	drperrors.NewResponse(http.StatusBadGateway).WriteTo(ctx, w)
}

func proxyRecover(ctx context.Context, w http.ResponseWriter, hcSvc healthchecksService) {
	r := recover()
	// special case for https://github.com/golang/go/issues/28239
	if r == nil || r == http.ErrAbortHandler { //nolint:errorlint // r is not a error
		return
	}
	apm.Log(ctx).Error("recovering from panic", "error", r)
	if hcSvc != nil {
		hcSvc.Fail(strings.NewReader(fmt.Sprintf("panic in proxy handler: %+v", r)))
	}
	drperrors.NewResponse(http.StatusInternalServerError).WriteTo(ctx, w)
}
