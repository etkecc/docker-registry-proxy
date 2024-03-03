package controllers

import (
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"gitlab.com/etke.cc/go/apm"
)

func ctxLog(c echo.Context) *zerolog.Logger {
	var host string
	if authorizedHost, ok := c.Get("host").(string); ok {
		host = authorizedHost
	}
	var cachekey string
	if cacheKey, ok := c.Get("cache.key").(string); ok {
		cachekey = cacheKey
	}

	log := apm.Log(c.Request().Context()).With().
		Str("req.method", c.Request().Method).
		Str("req.url", c.Request().URL.String()).
		Any("req.headers", c.Request().Header).
		Str("from.ip", c.RealIP()).
		Str("from.host", host).
		Str("cache.key", cachekey).
		Logger()
	return &log
}
