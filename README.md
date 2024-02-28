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
* **IAP_TARGET_SCHEME** - target scheme
* **IAP_TARGET_HOST** - target host
* **IAP_TRUSTEDIPS** - static list of trusted ips, space separated
