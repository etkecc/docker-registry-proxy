package controllers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
	"gitlab.com/etke.cc/go/apm"
	echobasicauth "gitlab.com/etke.cc/go/echo-basic-auth"

	"gitlab.com/etke.cc/docker-registry-proxy/config"
	"gitlab.com/etke.cc/docker-registry-proxy/errors"
	"gitlab.com/etke.cc/docker-registry-proxy/metrics"
	"gitlab.com/etke.cc/docker-registry-proxy/utils"
)

var httpTransport http.RoundTripper

type echoService interface {
	Middleware() echo.MiddlewareFunc
}

type healthchecksService interface {
	Fail(optionalBody ...io.Reader)
}

// ConfigureRouter configures echo router
func ConfigureRouter(e *echo.Echo, metricsAuth *echobasicauth.Auth, authSvc, cacheSvc echoService, hcSvc healthchecksService, target config.Target) {
	httpTransport = apm.WrapRoundTripper(http.DefaultTransport, apm.WithMaxRetries(0))
	e.Use(middleware.Recover())
	e.Use(middleware.Secure())
	e.Use(apm.WithSentry())
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set(echo.HeaderReferrerPolicy, "origin")
			return next(c)
		}
	})
	e.HideBanner = true
	e.IPExtractor = echo.ExtractIPFromXFFHeader(
		echo.TrustLoopback(true),
		echo.TrustLinkLocal(true),
		echo.TrustPrivateNet(true),
	)
	metricsAuthMiddleware := echobasicauth.NewMiddleware(metricsAuth)
	e.GET("/_health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	e.GET("/metrics", metrics.Handler(), metricsAuthMiddleware)

	e.Any("*", proxy(target, hcSvc), authSvc.Middleware(), cacheSvc.Middleware())
}

func proxy(target config.Target, hcSvc healthchecksService) echo.HandlerFunc {
	return func(c echo.Context) error {
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
				locationURL, err := url.Parse(location)
				if err == nil && locationURL.Host == target.Host {
					locationURL.Host = src.Host
				}
				r.Header.Set("Location", locationURL.String())
			}
			c.Set("resp.status", r.StatusCode)
			log.Info().
				Int("resp.status", r.StatusCode).
				Str("req.url", r.Request.URL.String()).
				Msg("proxied")
			return nil
		}

		defer proxyRecover(ctx, c.Response().Writer, hcSvc)
		proxy.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	}
}

func proxyError(w http.ResponseWriter, r *http.Request, hcSvc healthchecksService, err error) {
	var ctx context.Context
	var log zerolog.Logger
	if r != nil {
		ctx = r.Context()
		log = apm.Log(ctx).With().
			Str("method", r.Method).
			Str("url", r.URL.String()).
			Logger()
	} else {
		ctx = apm.NewContext()
		log = *apm.Log(ctx)
	}

	defer proxyRecover(ctx, w, hcSvc)
	log.Warn().Err(err).Msg("failed")
	if hcSvc != nil {
		hcSvc.Fail(strings.NewReader(fmt.Sprintf("%s %s failed: %+v", r.Method, r.URL.String(), err)))
	}
	errors.NewResponse(http.StatusBadGateway).WriteTo(ctx, w)
}

func proxyRecover(ctx context.Context, w http.ResponseWriter, hcSvc healthchecksService) {
	r := recover()
	// special case for https://github.com/golang/go/issues/28239
	if r == nil || r == http.ErrAbortHandler { //nolint:errorlint // r is not a error
		return
	}
	apm.Log(ctx).Error().Interface("error", r).Msg("recovering from panic")
	if hcSvc != nil {
		hcSvc.Fail(strings.NewReader(fmt.Sprintf("panic in proxy handler: %+v", r)))
	}
	errors.NewResponse(http.StatusInternalServerError).WriteTo(ctx, w)
}
