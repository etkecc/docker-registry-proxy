package utils

import (
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"gitlab.com/etke.cc/go/apm"
)

// NewMap creates a map from a slice of keys to a single value.
func NewMap[T comparable, V any](slice []T, value V) map[T]V {
	m := make(map[T]V, len(slice))
	for _, k := range slice {
		m[k] = value
	}
	return m
}

// NewLog creates a new logger with context from echo.Context
func NewLog(c echo.Context) *zerolog.Logger {
	logCtx := apm.Log(c.Request().Context()).With().
		Str("method", c.Request().Method).
		Str("url", c.Request().URL.String()).
		Str("ip", c.RealIP())

	log := logCtx.Logger()
	return &log
}
