package controllers

import (
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/mileusna/useragent"
	"github.com/rs/zerolog"
	"gitlab.com/etke.cc/go/apm"
	"gitlab.com/etke.cc/go/psd"
	"gitlab.com/etke.cc/int/iap/config"
)

var (
	trustedIPs    map[string]bool
	allowedUAs    map[string]bool
	cacheOK       *expirable.LRU[string, string]
	cacheNOK      *expirable.LRU[string, bool]
	httpTransport http.RoundTripper
	// cacheableStatuses = map[int]bool{
	// 	http.StatusOK:        true,
	// 	http.StatusNoContent: true,
	// }
	// cacheableEndpoints = []*regexp.Regexp{
	// 	regexp.MustCompile(`^GET /v2/$`),
	// 	regexp.MustCompile(`^GET /v2/_catalog$`),
	// 	regexp.MustCompile(`^GET /v2/_catalog\?n\=[0-9]*$`),
	// 	regexp.MustCompile(`^GET /v2/.*/tags/list$`),
	// 	regexp.MustCompile(`^GET /v2/.*/tags/list\?n\=[0-9]*$`),
	// 	regexp.MustCompile(`^HEAD /v2/.*/manifests/.*$`),
	// 	regexp.MustCompile(`^HEAD /v2/.*/blobs/sha256:[a-zA-Z0-9-_]*$`),
	// }
)

func initAuth(allowed config.Allowed) {
	trustedIPs = make(map[string]bool, len(allowed.IPs))
	for _, ip := range allowed.IPs {
		trustedIPs[ip] = true
	}
	allowedUAs = make(map[string]bool, len(allowed.UAs))
	for _, name := range allowed.UAs {
		allowedUAs[name] = true
	}

	cacheOK = expirable.NewLRU[string, string](1000, nil, 1*time.Hour)
	cacheNOK = expirable.NewLRU[string, bool](10000, nil, 1*time.Hour)
}

// ConfigureRouter configures echo router
func ConfigureRouter(e *echo.Echo, psdc *psd.Client, target config.Target, allowed config.Allowed) {
	httpTransport = apm.WrapRoundTripper(http.DefaultTransport)
	initAuth(allowed)
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
	return false
}

func authFull(c echo.Context, ip string, psdc *psd.Client, log *zerolog.Logger) (string, *echo.HTTPError) {
	if cacheNOK.Contains(ip) {
		log.Info().Str("reason", "cached NOK").Msg("rejected")
		return "", echo.NewHTTPError(http.StatusPaymentRequired, "Payment required")
	}

	if !allowedUAs[useragent.Parse(c.Request().UserAgent()).Name] {
		log.Info().Str("reason", "UA name is not allowed").Msg("rejected")
		return "", echo.NewHTTPError(http.StatusForbidden, "Forbidden")
	}

	// check PSD
	if psdc == nil {
		log.Error().Msg("No PSD client")
		return "", echo.NewHTTPError(http.StatusInternalServerError, "Cannot authenticate")
	}
	targets, err := psdc.GetWithContext(c.Request().Context(), ip)
	if err != nil {
		if strings.Contains(err.Error(), "410 Gone") {
			log.Info().Str("reason", "no targets").Msg("rejected")
			return "", echo.NewHTTPError(http.StatusPaymentRequired, "Payment required")
		}
		log.Error().Err(err).Msg("Failed to get targets")
		return "", echo.NewHTTPError(http.StatusInternalServerError, "Failed to authenticate")
	}
	// if no targets, add IP to NOK cache and return 402
	if len(targets) == 0 {
		log.Info().Str("reason", "no targets").Msg("rejected")
		return "", echo.NewHTTPError(http.StatusPaymentRequired, "Payment required")
	}

	log.Debug().Int("targets", len(targets)).Msg("Targets found")
	return targets[0].GetDomain(), nil
}

func auth(psdc *psd.Client) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// get IP first
			ip := c.RealIP()
			log := apm.Log(c.Request().Context()).With().
				Any("req.headers", c.Request().Header).
				Str("req.method", c.Request().Method).
				Str("req.url", c.Request().URL.String()).
				Str("from.ip", ip).
				Logger()
			if ip == "" {
				log.Error().Msg("Failed to get client IP")
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get real IP")
			}

			// cheap auth - i.e., trusted, cached, etc.
			if authCheap(ip, &log) {
				return next(c)
			}

			// full auth - i.e., UA, PSD, etc.
			host, err := authFull(c, ip, psdc, &log)
			if host == "" {
				cacheNOK.Add(ip, true)
				return err
			}

			cacheOK.Add(ip, host)
			return next(c)
		}
	}
}

func proxy(target config.Target) echo.HandlerFunc {
	return func(c echo.Context) error {
		src := *c.Request().URL
		src.Host = c.Request().Host
		authorizedHost, _ := cacheOK.Get(c.RealIP())
		log := apm.Log(c.Request().Context()).With().
			Str("req.method", c.Request().Method).
			Str("req.url", c.Request().URL.String()).
			Str("from.ip", c.RealIP()).
			Str("from.host", authorizedHost).
			Logger()

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
			log.Info().Int("resp.status", r.StatusCode).Msg("proxied")
			return nil
		}
		proxy.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	}
}
