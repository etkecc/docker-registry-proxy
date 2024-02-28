package controllers

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
	"gitlab.com/etke.cc/go/apm"
	"gitlab.com/etke.cc/go/psd"
	"gitlab.com/etke.cc/int/iap/config"
)

var (
	trustedIPs map[string]bool
	cacheOK    *expirable.LRU[string, bool]
	cacheNOK   *expirable.LRU[string, bool]
)

func initAuth(trusted []string) {
	trustedIPs = make(map[string]bool, len(trusted))
	for _, ip := range trusted {
		trustedIPs[ip] = true
	}

	cacheOK = expirable.NewLRU[string, bool](1000, nil, 1*time.Hour)
	cacheNOK = expirable.NewLRU[string, bool](10000, nil, 1*time.Hour)
}

// ConfigureRouter configures echo router
func ConfigureRouter(e *echo.Echo, psdc *psd.Client, target config.Target, trusted []string) {
	initAuth(trusted)

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Skipper: func(c echo.Context) bool {
			return c.Request().URL.Path == "/_health"
		},
		Format:           `${remote_ip} - - [${time_custom}] "${method} ${path} ${protocol}" ${status} ${bytes_out} "${referer}" "${user_agent}"` + "\n",
		CustomTimeFormat: "2/Jan/2006:15:04:05 -0700",
	}))
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
	e.GET("/_health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	e.Any("*", proxy(target), auth(psdc))
}

// authCheap is a cheap way to validate request auth
// from trusted IPs or cache
func authCheap(ip string, log *zerolog.Logger) bool {
	if trustedIPs[ip] {
		log.Debug().Msg("Trusted IP")
		return true
	}
	if cacheOK.Contains(ip) {
		log.Debug().Msg("OK cache hit")
		return true
	}
	if cacheNOK.Contains(ip) {
		log.Debug().Msg("NOK cache hit")
		return false
	}
	return false
}

func authFull(c echo.Context, ip string, psdc *psd.Client, log *zerolog.Logger) (bool, *echo.HTTPError) {
	// check PSD
	if psdc == nil {
		log.Error().Msg("No PSD client")
		return false, echo.NewHTTPError(http.StatusInternalServerError, "Cannot authenticate")
	}
	targets, err := psdc.GetWithContext(c.Request().Context(), ip)
	if err != nil {
		if strings.Contains(err.Error(), "410 Gone") {
			log.Debug().Msg("No targets")
			cacheNOK.Add(ip, true)
			return false, echo.NewHTTPError(http.StatusPaymentRequired, "Payment required")
		}
		log.Error().Err(err).Msg("Failed to get targets")
		return false, echo.NewHTTPError(http.StatusInternalServerError, "Failed to authenticate")
	}
	// if no targets, add IP to NOK cache and return 402
	if len(targets) == 0 {
		log.Debug().Msg("No targets")
		cacheNOK.Add(ip, true)
		return false, echo.NewHTTPError(http.StatusPaymentRequired, "Payment required")
	}

	log.Debug().Int("targets", len(targets)).Msg("Targets found")
	// add IP to OK cache and pass the request
	cacheOK.Add(ip, true)
	return true, nil
}

func auth(psdc *psd.Client) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// get IP first
			ip := c.RealIP()
			log := apm.Log(c.Request().Context()).With().Str("ip", ip).Logger()
			if ip == "" {
				log.Error().Any("headers", c.Request().Header).Msg("Failed to get client IP")
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get real IP")
			}

			if authCheap(ip, &log) {
				return next(c)
			}
			ok, err := authFull(c, ip, psdc, &log)
			if ok {
				return next(c)
			}
			return err
		}
	}
}

func proxy(target config.Target) echo.HandlerFunc {
	return func(c echo.Context) error {
		src := c.Request().URL
		dst := &url.URL{
			Scheme: target.Scheme,
			Host:   target.Host,
		}
		log := apm.Log(c.Request().Context()).With().Str("src", src.String()).Str("dst", dst.String()).Logger()
		log.Debug().Msg("proxying")
		proxy := httputil.ReverseProxy{
			Transport: apm.WrapRoundTripper(http.DefaultTransport),
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(dst)
				r.Out.Host = target.Host
				log.Debug().Str("dst", r.Out.URL.String()).Msg("rewriting")
			},
		}
		proxy.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}
