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

* **IAP_PORT** - http port, default `8080`
* **IAP_LOGLEVEL** - log level, default `info`
* **IAP_SENTRY** - sentry dsn
* **IAP_METRICS_LOGIN** - metrics login
* **IAP_METRICS_PASSWORD** - metrics password
* **IAP_METRICS_IPS** - metrics ips, space separated
* **IAP_CACHE_TTL** - cache ttl in minutes, default: 60
* **IAP_CACHE_SIZE** - cache size, default: 1000
* **IAP_TARGET_SCHEME** - target scheme
* **IAP_TARGET_HOST** - target host
* **IAP_ALLOWED_IPS** - static list of allowed ips, space separated (GET, HEAD, OPTIONS requests)
* **IAP_ALLOWED_UAS** - static list of allowed user agents, space separated (GET, HEAD, OPTIONS requests)
* **IAP_TRUSTED_IPS** - static list of trusted ips, space separated (PATCH, POST, PUT, DELETE requests)

