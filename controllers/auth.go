package controllers

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/mileusna/useragent"
	"github.com/rs/zerolog"
	"gitlab.com/etke.cc/go/psd"
	"gitlab.com/etke.cc/int/iap/metrics"
)

func auth(psdc *psd.Client) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			log := ctxLog(c)
			// get IP first
			ip := c.RealIP()
			if ip == "" {
				log.Error().Msg("Failed to get client IP")
				go metrics.Auth(false)
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get real IP")
			}

			// cheap auth - i.e., trusted, cached, etc.
			if authCheap(c, ip, log) {
				go metrics.Auth(true)
				return next(c)
			}

			// full auth - i.e., UA, PSD, etc.
			if ok, err := authFull(c, ip, psdc, log); !ok {
				go metrics.Auth(false)
				cacheNOK.Add(ip, true)
				return err
			}

			go metrics.Auth(true)
			cacheOK.Add(ip, c.Get("host").(string)) //nolint:forcetypeassert // we know it's a string
			return next(c)
		}
	}
}

// authCheap is a cheap way to validate request auth
// from trusted IPs or cache
func authCheap(c echo.Context, ip string, log *zerolog.Logger) bool {
	if trustedIPs[ip] {
		log.Debug().Msg("Trusted IP")
		return true
	}
	authorizedHost, ok := cacheOK.Get(ip)
	if ok {
		c.Set("host", authorizedHost)
		log.Debug().Msg("OK cache hit")
		return true
	}

	return false
}

func authFull(c echo.Context, ip string, psdc *psd.Client, log *zerolog.Logger) (bool, *echo.HTTPError) {
	if cacheNOK.Contains(ip) {
		log.Info().Str("reason", "cached NOK").Msg("rejected")
		return false, echo.NewHTTPError(http.StatusPaymentRequired, "Payment required")
	}

	if !allowedUAs[useragent.Parse(c.Request().UserAgent()).Name] {
		log.Info().Str("reason", "UA name is not allowed").Msg("rejected")
		return false, echo.NewHTTPError(http.StatusForbidden, "Forbidden")
	}

	// check PSD
	if psdc == nil {
		log.Error().Msg("No PSD client")
		return false, echo.NewHTTPError(http.StatusInternalServerError, "Cannot authenticate")
	}
	targets, err := psdc.GetWithContext(c.Request().Context(), ip)
	if err != nil {
		if strings.Contains(err.Error(), "410 Gone") {
			log.Info().Str("reason", "no targets").Msg("rejected")
			return false, echo.NewHTTPError(http.StatusPaymentRequired, "Payment required")
		}
		log.Error().Err(err).Msg("Failed to get targets")
		return false, echo.NewHTTPError(http.StatusInternalServerError, "Failed to authenticate")
	}
	// if no targets, add IP to NOK cache and return 402
	if len(targets) == 0 {
		log.Info().Str("reason", "no targets").Msg("rejected")
		return false, echo.NewHTTPError(http.StatusPaymentRequired, "Payment required")
	}

	log.Debug().Int("targets", len(targets)).Msg("Targets found")
	c.Set("host", targets[0].GetDomain())
	return true, nil
}
