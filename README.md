# Inventory Auth Proxy

internal etke.cc service, not usable for any other purposes.

Pass-through proxy using [PSD](https://gitlab.com/etke.cc/psd) as info provider

## Config

env:

* **IAP_PORT** - http port, default `8080`
* **IAP_LOGLEVEL** - log level, default `info`
* **IAP_SENTRY** - sentry dsn
* **IAP_PSD_URL** - PSD url
* **IAP_PSD_LOGIN** - PSD login
* **IAP_PSD_PASSWORD** - PSD password
* **IAP_METRICS_LOGIN** - metrics login
* **IAP_METRICS_PASSWORD** - metrics password
* **IAP_METRICS_IPS** - metrics ips, space separated
* **IAP_TARGET_SCHEME** - target scheme
* **IAP_TARGET_HOST** - target host
* **IAP_ALLOWED_IPS** - static list of trusted ips, space separated
* **IAP_ALLOWED_UAS** - static list of allowed user agents, space separated
