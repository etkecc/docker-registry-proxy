package utils

import (
	"log/slog"

	"github.com/etkecc/go-apm/v2"
	"github.com/etkecc/go-kit"
	"github.com/labstack/echo/v5"
)

// NewMap creates a map from a slice of keys to a single value.
func NewMap[T comparable, V any](slice []T, value V) map[T]V {
	m := make(map[T]V, len(slice))
	for _, k := range slice {
		m[k] = value
	}
	return m
}

// NewLog returns the request logger carrying method, url and anonymized client IP.
func NewLog(c *echo.Context) *slog.Logger {
	return apm.Log(c.Request().Context()).With(
		"method", c.Request().Method,
		"url", c.Request().URL.String(),
		"ip", kit.AnonymizeIP(c.RealIP()),
	)
}
