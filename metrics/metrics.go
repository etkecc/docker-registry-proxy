package metrics

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/VictoriaMetrics/metrics"
)

var (
	requestsTotal = metrics.NewCounter("iap_requests_total")
	requestsHEAD  = metrics.NewCounter("iap_requests_head")
	requestsGET   = metrics.NewCounter("iap_requests_get")

	authFailures  = metrics.NewCounter("iap_auth_failures")
	authSuccesses = metrics.NewCounter("iap_auth_successes")

	cacheHit  = metrics.NewCounter("iap_cache_hits")
	cacheMiss = metrics.NewCounter("iap_cache_misses")

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

// Handler for metrics
type Handler struct{}

func (h *Handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	metrics.WritePrometheus(w, false)
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
			var tag string
			if strings.HasPrefix(part, "sha256:") {
				tag = "@" + part
			} else {
				tag = ":" + part
			}
			imageParts[len(imageParts)-1] = imageParts[len(imageParts)-1] + tag
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
	metrics.GetOrCreateCounter(fmt.Sprintf("iap_requests_image{image=%q}", image)).Inc()
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
