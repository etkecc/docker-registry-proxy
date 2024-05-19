package apm

import (
	"context"
	"fmt"
	"os"

	zlogsentry "github.com/archdx/zerolog-sentry"
	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
)

// Log returns a logger with the context provided, if no context is provided, it will return a logger with a new context
func Log(ctx ...context.Context) *zerolog.Logger {
	if len(ctx) > 0 {
		return zerolog.Ctx(ctx[0])
	}
	return zerolog.Ctx(NewContext())
}

// NewLogger returns a new logger with sentry integration (if possible)
func NewLogger(ctx context.Context, sentryOpts ...zlogsentry.WriterOption) *zerolog.Logger {
	var w zerolog.LevelWriter

	consoleWriter := zerolog.LevelWriterAdapter{
		Writer: zerolog.ConsoleWriter{
			Out:          os.Stdout,
			PartsExclude: []string{zerolog.TimestampFieldName},
		},
	}

	sentryWriter, err := newSentryWriter(ctx, sentryOpts...)
	if err == nil {
		w = zerolog.MultiLevelWriter(sentryWriter, consoleWriter)
	} else {
		w = consoleWriter
	}

	log := zerolog.New(w).With().Timestamp().Caller().Logger().Hook(hcHook)
	return &log
}

// NewLoggerPlain returns a new logger without sentry integration
func NewLoggerPlain() *zerolog.Logger {
	consoleWriter := zerolog.ConsoleWriter{
		Out:          os.Stdout,
		PartsExclude: []string{zerolog.TimestampFieldName},
	}

	log := zerolog.New(consoleWriter)
	return &log
}

func newSentryWriter(ctx context.Context, sentryOpts ...zlogsentry.WriterOption) (zerolog.LevelWriter, error) {
	if sentryDSN == "" {
		return nil, fmt.Errorf("sentry DSN not set")
	}

	if hub := sentry.GetHubFromContext(ctx); hub != nil && hub.Scope() != nil && hub.Client() != nil {
		return zlogsentry.NewWithHub(hub, getSentryOptions(sentryOpts...)...)
	}
	return zlogsentry.New(sentryDSN, getSentryOptions(sentryOpts...)...)
}

func getSentryOptions(sentryOpts ...zlogsentry.WriterOption) []zlogsentry.WriterOption {
	if len(sentryOpts) > 0 {
		return sentryOpts
	}

	return []zlogsentry.WriterOption{
		zlogsentry.WithBreadcrumbs(),
		zlogsentry.WithTracing(),
		zlogsentry.WithSampleRate(0.25),
		zlogsentry.WithTracingSampleRate(0.25),
		zlogsentry.WithRelease(sentryName + "@" + sentryVersion),
	}
}
