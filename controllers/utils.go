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

	var cached bool
	if _, ok := c.Get("cache.hit").(bool); ok {
		cached = true
	}

	logCtx := apm.Log(c.Request().Context()).With().
		Str("req.method", c.Request().Method).
		Str("req.url", c.Request().URL.String()).
		Any("req.headers", c.Request().Header).
		Str("from.ip", c.RealIP())

	if host != "" {
		logCtx = logCtx.Str("from.host", host)
	}
	if cachekey != "" {
		logCtx = logCtx.Str("cache.key", cachekey)
	}
	if cached {
		logCtx = logCtx.Bool("cache.hit", true)
	}

	log := logCtx.Logger()
	return &log
}
