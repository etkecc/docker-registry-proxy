package apm

import (
	"runtime/debug"

	"github.com/rs/zerolog"
)

var (
	loglevel      zerolog.Level
	sentryDSN     string
	sentryName    string
	sentryVersion = func() string {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					return setting.Value
				}
			}
		}
		return "development"
	}()
)

// SetName sets the name of the application
func SetName(name string) {
	sentryName = name
}

// SetLogLevel sets the log level
func SetLogLevel(level string) {
	var err error
	loglevel, err = zerolog.ParseLevel(level)
	if err != nil {
		loglevel = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(loglevel)
}

// SetSentryDSN sets the sentry DSN
func SetSentryDSN(dsn string) {
	sentryDSN = dsn
}
