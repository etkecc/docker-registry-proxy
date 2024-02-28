package apm

import (
	"fmt"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v4"
)

// WithSentry is a middleware that creates a new transaction for each request.
func WithSentry() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := NewContext(c.Request().Context())
			c.SetRequest(c.Request().WithContext(ctx))
			if hub := sentry.GetHubFromContext(ctx); hub != nil {
				hub.Scope().SetRequest(c.Request())
			}
			defer recoverWithSentry(c)

			if c.Request().URL.Path == "/_health" {
				return next(c)
			}

			options := []sentry.SpanOption{
				sentry.WithOpName("http.server"),
				sentry.ContinueFromRequest(c.Request()),
				sentry.WithTransactionSource(sentry.SourceURL),
			}

			path := c.Path()
			if path == "" || path == "/" {
				path = c.Request().URL.Path
			}

			transaction := sentry.StartTransaction(c.Request().Context(),
				fmt.Sprintf("%s %s", c.Request().Method, path),
				options...,
			)
			defer transaction.Finish()

			c.SetRequest(c.Request().WithContext(transaction.Context()))
			if err := next(c); err != nil {
				transaction.Status = sentry.HTTPtoSpanStatus(c.Response().Status)
				return err
			}
			transaction.Status = sentry.SpanStatusOK
			return nil
		}
	}
}

func recoverWithSentry(c echo.Context) {
	if err := recover(); err != nil {
		sentry.GetHubFromContext(c.Request().Context()).Recover(err)
	}
}
