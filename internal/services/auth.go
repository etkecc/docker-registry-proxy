package services

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/labstack/echo/v4"
	"github.com/mileusna/useragent"
	"github.com/rs/zerolog"

	"github.com/etkecc/docker-registry-proxy/internal/errors"
	"github.com/etkecc/docker-registry-proxy/internal/metrics"
	"github.com/etkecc/docker-registry-proxy/internal/utils"
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
	cacheAllowedOK  *expirable.LRU[string, bool]
	cacheAllowedNOK *expirable.LRU[string, bool]
	provider        *AuthProvider
}

// NewAuth creates a new Auth service
func NewAuth(allowedIPs, allowedUAs, trustedIPs []string, cacheTTL, cacheSize int, provider *AuthProvider) *Auth {
	return &Auth{
		provider:        provider,
		allowedIPs:      utils.NewMap(allowedIPs, true),
		allowedUAs:      utils.NewMap(allowedUAs, true),
		trustedIPs:      utils.NewMap(trustedIPs, true),
		cacheAllowedOK:  expirable.NewLRU[string, bool](cacheSize, nil, time.Duration(cacheTTL)*time.Minute),
		cacheAllowedNOK: expirable.NewLRU[string, bool](cacheSize, nil, time.Duration(cacheTTL)*time.Minute),
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
			return c.JSON(http.StatusMethodNotAllowed, errors.NewResponse(http.StatusMethodNotAllowed, fmt.Sprintf("Method %s is not allowed for IP %s", c.Request().Method, ip)))
		}
	}
}

func (a *Auth) middlewareAllowed(c echo.Context, ip string, log *zerolog.Logger, next echo.HandlerFunc) error {
	if a.allowedFromCache(ip, log) {
		go metrics.Auth(ip, true)
		return next(c)
	}
	if ok := a.allowedFull(c, ip, log); !ok {
		go metrics.Auth(ip, false)
		a.cacheAllowedNOK.Add(ip, true)
		return c.JSON(http.StatusPaymentRequired, errors.NewResponse(http.StatusPaymentRequired, fmt.Sprintf("Method %s is not allowed for IP %s", c.Request().Method, ip)))
	}

	go metrics.Auth(ip, true)
	a.cacheAllowedOK.Add(ip, true)
	return next(c)
}

func (a *Auth) middlewareTrusted(c echo.Context, ip string, log *zerolog.Logger, next echo.HandlerFunc) error {
	if a.trustedIPs[ip] {
		log.Debug().Msg("trusted IP")
		go metrics.Auth(ip, true)
		return next(c)
	}

	log.Info().Str("reason", "IP is not trusted").Msg("rejected")
	go metrics.Auth(ip, false)
	return c.JSON(http.StatusForbidden, errors.NewResponse(http.StatusForbidden, fmt.Sprintf("Method %s is not allowed for IP %s", c.Request().Method, ip)))
}

func (a *Auth) allowedFromCache(ip string, log *zerolog.Logger) bool {
	if a.allowedIPs[ip] {
		log.Debug().Msg("allowed IP")
		return true
	}

	if _, ok := a.cacheAllowedOK.Get(ip); ok {
		log.Debug().Msg("OK cache hit")
		return true
	}

	return false
}

func (a *Auth) allowedFull(c echo.Context, ip string, log *zerolog.Logger) bool {
	if _, ok := a.cacheAllowedNOK.Get(ip); ok {
		log.Info().Str("reason", "cached NOK").Msg("rejected")
		return false
	}

	ua := useragent.Parse(c.Request().UserAgent()).Name
	if !a.allowedUAs[ua] {
		log.Info().Str("reason", "UA name is not allowed").Str("ua", ua).Msg("rejected")
		return false
	}

	if a.provider == nil {
		log.Debug().Msg("Auth Provider is not configured")
		return true
	}

	ok, err := a.provider.IsAllowed(c.Request().Context(), ip)
	if !ok {
		log.Info().Str("reason", err.Error()).Msg("rejected")
		return false
	}

	return true
}
