# Docker Registry Proxy

Pass-through docker registry (distribution) proxy with the following features:

* docker-compatible errors
* metadata caching (up to 100% cache hit ratio on supported endpoints and http methods)
* prometheus metrics with basic auth and ip filtering
* sentry integration
* ip filtering (GET, HEAD, OPTIONS) and trust (PATCH, POST, PUT, DELETE)
* user agent filtering
* configurable backend (including private networks)

## Config

env:

* **DRP_PORT** - http port, default `8080`
* **DRP_LOGLEVEL** - log level, default `info`
* **DRP_SENTRY** - sentry dsn
* **DRP_METRICS_LOGIN** - metrics login
* **DRP_METRICS_PASSWORD** - metrics password
* **DRP_METRICS_IPS** - metrics ips, space separated
* **DRP_CACHE_TTL** - cache ttl in minutes, default: 60
* **DRP_CACHE_SIZE** - cache size, default: 1000
* **DRP_TARGET_SCHEME** - target scheme
* **DRP_TARGET_HOST** - target host
* **DRP_ALLOWED_IPS** - static list of allowed ips, space separated (GET, HEAD, OPTIONS requests)
* **DRP_ALLOWED_UAS** - static list of allowed user agents, space separated (GET, HEAD, OPTIONS requests)
* **DRP_TRUSTED_IPS** - static list of trusted ips, space separated (PATCH, POST, PUT, DELETE requests)

