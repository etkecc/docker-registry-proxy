package controllers

import (
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gitlab.com/etke.cc/go/apm"
	echobasicauth "gitlab.com/etke.cc/go/echo-basic-auth"
	"gitlab.com/etke.cc/int/iap/config"
	"gitlab.com/etke.cc/int/iap/metrics"
	"gitlab.com/etke.cc/int/iap/utils"
)

var httpTransport http.RoundTripper

type echoService interface {
	Middleware() echo.MiddlewareFunc
}

// ConfigureRouter configures echo router
func ConfigureRouter(e *echo.Echo, metricsAuth *echobasicauth.Auth, authSvc, cacheSvc echoService, target config.Target) {
	httpTransport = apm.WrapRoundTripper(http.DefaultTransport)
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

	e.Any("*", proxy(target), authSvc.Middleware(), cacheSvc.Middleware())
}

func proxy(target config.Target) echo.HandlerFunc {
	return func(c echo.Context) error {
		src := *c.Request().URL
		src.Host = c.Request().Host
		log := utils.NewLog(c)
		c.Request().Host = target.Host

		proxy := httputil.NewSingleHostReverseProxy(&url.URL{Host: target.Host, Scheme: target.Scheme})
		proxy.Transport = httpTransport
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			bodyb, _ := io.ReadAll(r.Body) //nolint:errcheck // ignore error
			log.Warn().Err(err).Str("resp.body", string(bodyb)).Msg("failed")
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
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
		proxy.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	}
}
