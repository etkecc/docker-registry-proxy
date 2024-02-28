package config

import (
	"gitlab.com/etke.cc/go/env"
)

const prefix = "iap"

// Config for IAP service
type Config struct {
	Port       string   // http port
	LogLevel   string   // log level
	SentryDSN  string   // sentry dsn
	PSD        PSD      // PSD config
	Target     Target   // target config
	TrustedIPs []string // trusted ips
}

type PSD struct {
	URL      string
	Login    string
	Password string
}

type Target struct {
	Scheme string
	Host   string
}

// New config
func New() *Config {
	env.SetPrefix(prefix)

	return &Config{
		Port:      env.String("port", "8080"),
		LogLevel:  env.String("loglevel", "info"),
		SentryDSN: env.String("sentry"),
		PSD: PSD{
			URL:      env.String("psd.url"),
			Login:    env.String("psd.login"),
			Password: env.String("psd.password"),
		},
		Target: Target{
			Scheme: env.String("target.scheme"),
			Host:   env.String("target.host"),
		},
		TrustedIPs: env.Slice("trustedips"),
	}
}
