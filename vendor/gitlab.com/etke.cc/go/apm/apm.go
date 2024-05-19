package apm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

// Error captures the error and sends it to sentry and healthchecks
func Error(ctx context.Context, err error) {
	if err == nil {
		return
	}

	GetHub(ctx).CaptureException(err)
	HealthcheckFail(strings.NewReader("error: " + err.Error()))
}

// StartSpan starts a new span, and if there is no transaction, it starts a new transaction
func StartSpan(ctx context.Context, operation string) *sentry.Span {
	if transaction := sentry.TransactionFromContext(ctx); transaction == nil {
		ctx = sentry.StartTransaction(ctx, operation, sentry.WithDescription(operation)).Context()
	}
	return sentry.StartSpan(ctx, operation, sentry.WithDescription(operation))
}

// GetHub returns the hub from the context (if context is provided and has a hub) or the current hub
func GetHub(ctx ...context.Context) *sentry.Hub {
	if len(ctx) == 0 {
		return sentry.CurrentHub()
	}

	if hub := sentry.GetHubFromContext(ctx[0]); hub != nil {
		return hub
	}

	return sentry.CurrentHub()
}

// Flush sends the events to sentry
func Flush(ctx ...context.Context) {
	GetHub(ctx...).Flush(5 * time.Second)
}

// Recover sends the error to sentry
func Recover(err any, repanic bool, ctx ...context.Context) {
	if err == nil {
		return
	}
	HealthcheckFail(strings.NewReader(fmt.Sprintf("panic recovered: %+v", err)))
	GetHub(ctx...).Recover(err)
	Flush(ctx...)
	if repanic {
		panic(err)
	}
}
