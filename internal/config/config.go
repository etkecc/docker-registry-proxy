package config

import (
	echobasicauth "github.com/etkecc/go-echo-basic-auth"
	"github.com/etkecc/go-env"
)

const prefix = "drp"

// Config for DRP service
type Config struct {
	Port         string              // http port
	LogLevel     string              // log level
	SentryDSN    string              // sentry dsn
	Healthchecks Healthchecks        // healthchecks config
	Target       Target              // target config
	Cache        Cache               // cache config
	Allowed      Allowed             // allowed ips and user agents (GET, HEAD, OPTIONS requests only)
	Trusted      Trusted             // trusted ips (PATCH, POST, PUT, DELETE requests)
	Metrics      *echobasicauth.Auth // metrics basic auth
}

// Healthchecks.io config
type Healthchecks struct {
	URL  string
	UUID string
}

// Allowed config (GET, HEAD, OPTIONS requests only)
type Allowed struct {
	IPs      []string     // static list of allowed IPs - requests from those IPS will be allowed
	UAs      []string     // only those user agents' names will be allowed, all other will be rejected
	Provider AuthProvider // auth provider
}

// Trusted config (PATCH, POST, PUT, DELETE requests)
type Trusted struct {
	IPs []string // static list of trusted IPs - requests from those IPS will be allowed
}

// Cache config
type Cache struct {
	Disabled bool // cache disabled
	TTL      int  // cache TTL in minutes
	Size     int  // cache size
}

// Target (backend) config
type Target struct {
	Scheme string
	Host   string
}

type AuthProvider struct {
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
		Healthchecks: Healthchecks{
			URL:  env.String("hc.url", "https://hc-ping.com"),
			UUID: env.String("hc.uuid"),
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
			Disabled: env.Bool("cache.disabled"),
			TTL:      env.Int("cache.ttl", 60),
			Size:     env.Int("cache.size", 1000),
		},
		Allowed: Allowed{
			IPs: env.Slice("allowed.ips"),
			UAs: env.Slice("allowed.uas"),
			Provider: AuthProvider{
				URL:      env.String("allowed.provider.url"),
				Login:    env.String("allowed.provider.login"),
				Password: env.String("allowed.provider.password"),
			},
		},
		Trusted: Trusted{
			IPs: env.Slice("trusted.ips"),
		},
	}
}
