package apm

import (
	"context"

	"github.com/getsentry/sentry-go"
)

// StartSpan starts a new span, and if there is no transaction, it starts a new transaction
func StartSpan(ctx context.Context, operation string) *sentry.Span {
	if transaction := sentry.TransactionFromContext(ctx); transaction == nil {
		ctx = sentry.StartTransaction(ctx, operation, sentry.WithDescription(operation)).Context()
	}
	return sentry.StartSpan(ctx, operation, sentry.WithDescription(operation))
}
