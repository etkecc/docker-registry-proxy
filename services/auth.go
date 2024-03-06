package services

import (
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/labstack/echo/v4"
	"github.com/mileusna/useragent"
	"github.com/rs/zerolog"
	"gitlab.com/etke.cc/go/psd"
	"gitlab.com/etke.cc/int/iap/errors"
	"gitlab.com/etke.cc/int/iap/metrics"
	"gitlab.com/etke.cc/int/iap/utils"
)

var (
	allowedMethods = utils.NewMap([]string{"GET", "HEAD", "OPTIONS"}, true)
	trustedMethods = utils.NewMap([]string{"PATCH", "POST", "PUT", "DELETE"}, true)
)

// Auth is a service for authentication
// it breaks into 2 "modes" - allowed and trusted
// allowed mode is for "read" requests (GET, HEAD, OPTIONS)
// trusted mode is for "write" requests (PATCH, POST, PUT, DELETE)
type Auth struct {
	allowedIPs      map[string]bool
	allowedUAs      map[string]bool
	trustedIPs      map[string]bool
	cacheAllowedOK  *expirable.LRU[string, string]
	cacheAllowedNOK *expirable.LRU[string, bool]
	psdc            *psd.Client
}

// NewAuth creates a new Auth service
func NewAuth(allowedIPs, allowedUAs, trustedIPs []string, cacheTTL, cacheSize int, psdc *psd.Client) *Auth {
	return &Auth{
		allowedIPs:      utils.NewMap(allowedIPs, true),
		allowedUAs:      utils.NewMap(allowedUAs, true),
		trustedIPs:      utils.NewMap(trustedIPs, true),
		cacheAllowedOK:  expirable.NewLRU[string, string](cacheSize, nil, time.Duration(cacheTTL)*time.Minute),
		cacheAllowedNOK: expirable.NewLRU[string, bool](cacheSize, nil, time.Duration(cacheTTL)*time.Minute),
		psdc:            psdc,
	}
}

// Middleware returns a middleware for echo
func (a *Auth) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			log := utils.NewLog(c)
			ip := c.RealIP()
			if ip == "" {
				log.Error().Msg("Failed to get client IP")
				return c.JSON(http.StatusInternalServerError, errors.NewResponse(http.StatusInternalServerError))
			}

			if allowedMethods[c.Request().Method] {
				return a.middlewareAllowed(c, ip, log, next)
			}
			if trustedMethods[c.Request().Method] {
				return a.middlewareTrusted(c, ip, log, next)
			}
			log.Info().Str("reason", "method not allowed").Msg("rejected")
			return c.JSON(http.StatusMethodNotAllowed, errors.NewResponse(http.StatusMethodNotAllowed))
		}
	}
}

func (a *Auth) middlewareAllowed(c echo.Context, ip string, log *zerolog.Logger, next echo.HandlerFunc) error {
	if a.allowedFromCache(c, ip, log) {
		go metrics.Auth(false)
		return next(c)
	}
	ok, statusCode := a.allowedFull(c, ip, log)
	if !ok {
		go metrics.Auth(false)
		a.cacheAllowedNOK.Add(ip, true)
		return c.JSON(statusCode, errors.NewResponse(statusCode))
	}

	go metrics.Auth(true)
	a.cacheAllowedOK.Add(ip, c.Get("host").(string)) //nolint:forcetypeassert // we know it's a string
	return next(c)
}

func (a *Auth) middlewareTrusted(c echo.Context, ip string, log *zerolog.Logger, next echo.HandlerFunc) error {
	if a.trustedIPs[ip] {
		log.Debug().Msg("trusted IP")
		go metrics.Auth(true)
		return next(c)
	}

	log.Info().Str("reason", "IP is not trusted").Msg("rejected")
	go metrics.Auth(false)
	return c.JSON(http.StatusForbidden, errors.NewResponse(http.StatusForbidden))
}

func (a *Auth) allowedFromCache(c echo.Context, ip string, log *zerolog.Logger) bool {
	if a.allowedIPs[ip] {
		log.Debug().Msg("allowed IP")
		return true
	}
	authorizedHost, ok := a.cacheAllowedOK.Get(ip)
	if ok {
		c.Set("host", authorizedHost)
		log.Debug().Msg("OK cache hit")
		return true
	}

	return false
}

func (a *Auth) allowedFull(c echo.Context, ip string, log *zerolog.Logger) (ok bool, statusCode int) {
	if a.cacheAllowedNOK.Contains(ip) {
		log.Info().Str("reason", "cached NOK").Msg("rejected")
		return false, http.StatusPaymentRequired
	}

	if !a.allowedUAs[useragent.Parse(c.Request().UserAgent()).Name] {
		log.Info().Str("reason", "UA name is not allowed").Msg("rejected")
		return false, http.StatusForbidden
	}

	if a.psdc == nil {
		log.Debug().Msg("PSD client is not available")
		return true, http.StatusOK
	}

	targets, err := a.psdc.GetWithContext(c.Request().Context(), ip)
	if err != nil {
		if strings.Contains(err.Error(), "410 Gone") {
			log.Info().Str("reason", "no targets").Msg("rejected")
			return false, http.StatusPaymentRequired
		}
		log.Error().Err(err).Msg("Failed to get targets")
		return false, http.StatusInternalServerError
	}
	// if no targets, add IP to NOK cache and return 402
	if len(targets) == 0 {
		log.Info().Str("reason", "no targets").Msg("rejected")
		return false, http.StatusPaymentRequired
	}

	log.Debug().Int("targets", len(targets)).Msg("Targets found")
	c.Set("host", targets[0].GetDomain())
	return true, http.StatusOK
}
