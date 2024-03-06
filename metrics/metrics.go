package metrics

import (
	"fmt"
	"strings"

	"github.com/VictoriaMetrics/metrics"
	"github.com/labstack/echo/v4"
)

var (
	requestsTotal = metrics.NewCounter("drp_requests_total")
	requestsHEAD  = metrics.NewCounter("drp_requests_head")
	requestsGET   = metrics.NewCounter("drp_requests_get")

	authFailures  = metrics.NewCounter("drp_auth_failures")
	authSuccesses = metrics.NewCounter("drp_auth_successes")

	cacheHit  = metrics.NewCounter("drp_cache_hits")
	cacheMiss = metrics.NewCounter("drp_cache_misses")

	notImages = map[string]bool{
		"":         true,
		"/v2":      true,
		"_catalog": true,
	}
	suffixes = map[string]bool{
		"blobs": true,
		"tags":  true,
	}
)

// Handler returns an echo handler that writes prometheus metrics to the response
func Handler() echo.HandlerFunc {
	return func(c echo.Context) error {
		metrics.WritePrometheus(c.Response(), false)
		return nil
	}
}

func extractName(reqURL string) string {
	imageParts := []string{}
	reqURL = strings.TrimPrefix(reqURL, "/v2/")
	var withManifest bool
	parts := strings.Split(reqURL, "/")
	for _, part := range parts {
		if suffixes[part] {
			break
		}
		if part == "manifests" {
			withManifest = true
			continue
		}

		if withManifest {
			if strings.HasPrefix(part, "sha256:") { // we don't want to count specific blobs, only tags
				return ""
			}

			imageParts[len(imageParts)-1] = imageParts[len(imageParts)-1] + ":" + part
			continue
		}

		imageParts = append(imageParts, part)
	}

	// if the request is not for a specific manifest, we don't want to count it
	if !withManifest {
		return ""
	}

	return strings.Join(imageParts, "/")
}

// Request increments the total requests counter and the specific method counter
// plus, it tries to parse image name from the URL path, and increments the specific image counter
func Request(method, path string) {
	requestsTotal.Inc()
	switch method {
	case "HEAD":
		requestsHEAD.Inc()
	case "GET":
		requestsGET.Inc()
	}

	image := extractName(path)
	if notImages[image] {
		return
	}
	metrics.GetOrCreateCounter(fmt.Sprintf("drp_requests_image{image=%q}", image)).Inc()
}

// Auth increments the auth successes or failures counter
func Auth(success bool) {
	if success {
		authSuccesses.Inc()
	} else {
		authFailures.Inc()
	}
}

// Cache increments the cache hits or misses counter
func Cache(hit bool) {
	if hit {
		cacheHit.Inc()
	} else {
		cacheMiss.Inc()
	}
}
