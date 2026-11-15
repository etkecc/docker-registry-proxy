package metrics

import "testing"

// TestExtractName pins the paths that used to panic on an empty repository list.
func TestExtractName(t *testing.T) {
	tests := map[string]string{
		"/v2/manifests/latest":                    "",
		"/v2/manifests/":                          "",
		"/v2/manifests/foo":                       "",
		"/v2/":                                    "",
		"/v2/library/alpine/manifests/latest":     "library/alpine:latest",
		"/v2/library/alpine/manifests/sha256:abc": "",
		"/v2/library/alpine/blobs/sha256:abc":     "",
		"/v2/library/alpine/tags/list":            "",
	}
	for path, want := range tests {
		if got := extractName(path); got != want {
			t.Errorf("extractName(%q) = %q, want %q", path, got, want)
		}
	}
}
