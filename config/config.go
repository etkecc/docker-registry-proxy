package config

import (
	"gitlab.com/etke.cc/go/env"
)

const prefix = "iap"

// Config for IAP service
type Config struct {
	Port      string  // http port
	LogLevel  string  // log level
	SentryDSN string  // sentry dsn
	PSD       PSD     // PSD config
	Target    Target  // target config
	Allowed   Allowed // allowed ips and user agents
}

type Allowed struct {
	IPs []string // trusted IPs - requests from those IPS will be allowed
	UAs []string // only those user agents' names will be allowed, all other will be rejected
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
		Allowed: Allowed{
			IPs: env.Slice("allowed.ips"),
			UAs: env.Slice("allowed.uas"),
		},
	}
}
