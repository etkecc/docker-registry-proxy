package config

import (
	echobasicauth "gitlab.com/etke.cc/go/echo-basic-auth"
	"gitlab.com/etke.cc/go/env"
)

const prefix = "iap"

// Config for IAP service
type Config struct {
	Port      string // http port
	LogLevel  string // log level
	SentryDSN string // sentry dsn
	PSD       PSD
	Target    Target              // target config
	Cache     Cache               // cache config
	Allowed   Allowed             // allowed ips and user agents (GET, HEAD, OPTIONS requests only)
	Trusted   Trusted             // trusted ips (PATCH, POST, PUT, DELETE requests)
	Metrics   *echobasicauth.Auth // metrics basic auth
}

// Allowed config (GET, HEAD, OPTIONS requests only)
type Allowed struct {
	IPs []string // static list of allowed IPs - requests from those IPS will be allowed
	UAs []string // only those user agents' names will be allowed, all other will be rejected
}

// Trusted config (PATCH, POST, PUT, DELETE requests)
type Trusted struct {
	IPs []string // static list of trusted IPs - requests from those IPS will be allowed
}

// Cache config
type Cache struct {
	TTL  int // cache TTL in minutes
	Size int // cache size
}

// Target (backend) config
type Target struct {
	Scheme string
	Host   string
}

type PSD struct {
	URL      string
	Login    string
	Password string
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
		Metrics: &echobasicauth.Auth{
			Login:    env.String("metrics.login"),
			Password: env.String("metrics.password"),
			IPs:      env.Slice("metrics.ips"),
		},
		Target: Target{
			Scheme: env.String("target.scheme"),
			Host:   env.String("target.host"),
		},
		Cache: Cache{
			TTL:  env.Int("cache.ttl", 60),
			Size: env.Int("cache.size", 1000),
		},
		Allowed: Allowed{
			IPs: env.Slice("allowed.ips"),
			UAs: env.Slice("allowed.uas"),
		},
		Trusted: Trusted{
			IPs: env.Slice("trusted.ips"),
		},
	}
}
