package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/etkecc/docker-registry-proxy/internal/config"
)

// TestReportExitPostsTheExitCode pins the ping the healthchecks client cannot deliver itself.
func TestReportExitPostsTheExitCode(t *testing.T) {
	observed := make(chan [2]string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		observed <- [2]string{r.URL.Path, string(raw)}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	reportExit(config.Healthchecks{URL: srv.URL, UUID: "check-1"}, 1, slog.New(slog.DiscardHandler))

	got := <-observed
	if got[0] != "/check-1/1" {
		t.Fatalf("path = %q, want /check-1/1", got[0])
	}
	if !strings.Contains(got[1], "code 1") {
		t.Fatalf("body = %q, want the exit description", got[1])
	}
}
