package controllers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	echobasicauth "github.com/etkecc/go-echo-basic-auth/v2"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/etkecc/docker-registry-proxy/internal/config"
	"github.com/etkecc/docker-registry-proxy/internal/services"
)

const (
	testLogin     = "admin"
	testPassword  = "secret"
	testUserAgent = "curl/8.5.0"
)

// logSink collects log output written by concurrently served requests.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// failingHealthchecks counts the failure reports the proxy sends upstream.
type failingHealthchecks struct {
	failures atomic.Int64
}

func (f *failingHealthchecks) Fail(...io.Reader) {
	f.failures.Add(1)
}

// newUpstream starts a fake registry that answers every request with its method and path.
func newUpstream(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"method":%q,"path":%q}`, r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	return srv, &hits
}

// newApp wires the production router against targetURL and serves it on a loopback socket.
func newApp(t *testing.T, targetURL string, hcSvc healthchecksService) (string, *logSink) {
	t.Helper()
	u, err := url.Parse(targetURL)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}

	sink := &logSink{}
	e := echo.New()
	e.Logger = slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelWarn}))

	authSvc := services.NewAuth([]string{"127.0.0.1", "::1"}, []string{"curl"}, []string{"127.0.0.1", "::1"}, 60, 1000, nil)
	cacheSvc := services.NewCache(true, 60, 1000)
	metricsAuth := &echobasicauth.Auth{Login: testLogin, Password: testPassword, IPs: []string{"127.0.0.1", "::1"}}
	if err = ConfigureRouter(e, metricsAuth, authSvc, cacheSvc, hcSvc, config.Target{Scheme: u.Scheme, Host: u.Host}); err != nil {
		t.Fatalf("configure router: %v", err)
	}

	return serve(t, e), sink
}

// serve runs e through echo.StartConfig on a random loopback port and stops it when the test ends.
func serve(t *testing.T, e *echo.Echo) string {
	t.Helper()
	addrs := make(chan net.Addr, 1)
	sc := echo.StartConfig{
		Address:          "127.0.0.1:0",
		HideBanner:       true,
		HidePort:         true,
		GracefulTimeout:  time.Second,
		ListenerAddrFunc: func(addr net.Addr) { addrs <- addr },
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- sc.Start(ctx, e) }()

	var addr net.Addr
	select {
	case addr = <-addrs:
	case err := <-done:
		cancel()
		t.Fatalf("server exited before listening: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("timed out waiting for the listener")
	}

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server stop: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for server shutdown")
		}
	})

	return "http://" + addr.String()
}

// request builds a request carrying the user agent the auth service expects.
func request(t *testing.T, base, method, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, base+path, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("User-Agent", testUserAgent)

	return req
}

// do sends req and returns status, headers and body, closing the response body.
func do(t *testing.T, req *http.Request) (status int, header http.Header, body string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return resp.StatusCode, resp.Header, string(raw)
}

func TestRouterHealthNeedsNoAuth(t *testing.T) {
	upstream, _ := newUpstream(t)
	base, _ := newApp(t, upstream.URL, nil)

	status, _, body := do(t, request(t, base, http.MethodGet, "/_health"))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("body = %q, want the ok payload", body)
	}
}

func TestRouterMetricsAuth(t *testing.T) {
	upstream, _ := newUpstream(t)
	base, sink := newApp(t, upstream.URL, nil)

	status, header, _ := do(t, request(t, base, http.MethodGet, "/metrics"))
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want %d", status, http.StatusUnauthorized)
	}
	if challenge := header.Get("WWW-Authenticate"); !strings.Contains(strings.ToLower(challenge), "basic") {
		t.Fatalf("WWW-Authenticate = %q, want a basic challenge", challenge)
	}

	req := request(t, base, http.MethodGet, "/metrics")
	req.SetBasicAuth(testLogin, "wrong")
	if status, _, _ = do(t, req); status != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want %d", status, http.StatusUnauthorized)
	}
	if !strings.Contains(sink.String(), "127.0.0.0 - FAIL") {
		t.Fatalf("log = %q, want a WARN record with the anonymized address", sink.String())
	}

	req = request(t, base, http.MethodGet, "/metrics")
	req.SetBasicAuth(testLogin, testPassword)
	status, _, body := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "drp_requests_total") {
		t.Fatalf("body = %q, want the metrics exposition", body)
	}
}

func TestRouterMetricsRejectsForwardedPeerOutsideAllowlist(t *testing.T) {
	upstream, _ := newUpstream(t)
	base, _ := newApp(t, upstream.URL, nil)

	req := request(t, base, http.MethodGet, "/metrics")
	req.SetBasicAuth(testLogin, testPassword)
	req.Header.Set("X-Forwarded-For", "8.8.8.8")

	status, _, body := do(t, req)
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d for a peer outside the allowlist", status, http.StatusUnauthorized)
	}
	if strings.Contains(body, "drp_requests_total") {
		t.Fatalf("body = %q, want the metrics exposition withheld", body)
	}
}

func TestRouterProxiesReadsAndWrites(t *testing.T) {
	upstream, _ := newUpstream(t)
	base, _ := newApp(t, upstream.URL, nil)

	status, _, body := do(t, request(t, base, http.MethodGet, "/v2/library/alpine/manifests/latest"))
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", status, http.StatusOK)
	}
	if !strings.Contains(body, `"method":"GET"`) || !strings.Contains(body, "/v2/library/alpine/manifests/latest") {
		t.Fatalf("GET body = %q, want the upstream answer for the manifest", body)
	}

	status, _, body = do(t, request(t, base, http.MethodPost, "/v2/library/alpine/blobs/uploads/"))
	if status != http.StatusOK {
		t.Fatalf("POST status = %d, want %d", status, http.StatusOK)
	}
	if !strings.Contains(body, `"method":"POST"`) {
		t.Fatalf("POST body = %q, want the upstream answer", body)
	}
}

// TestRouterKeepsFinalStatusAfterInformationalResponse guards 1xx from committing the echoed response.
func TestRouterKeepsFinalStatusAfterInformationalResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", "</style.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	t.Cleanup(upstream.Close)

	base, _ := newApp(t, upstream.URL, nil)
	status, _, body := do(t, request(t, base, http.MethodPut, "/v2/library/alpine/blobs/uploads/"))
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want %d: an informational response must not replace the final status", status, http.StatusCreated)
	}
	if body != "created" {
		t.Fatalf("body = %q, want %q", body, "created")
	}
}

func TestRouterRejectsUnknownMethod(t *testing.T) {
	upstream, _ := newUpstream(t)
	base, _ := newApp(t, upstream.URL, nil)

	status, _, body := do(t, request(t, base, http.MethodTrace, "/v2/"))
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", status, http.StatusMethodNotAllowed)
	}
	if !strings.Contains(body, "METHOD_NOT_ALLOWED") {
		t.Fatalf("body = %q, want the docker error payload", body)
	}
}

func TestRouterReportsBadGateway(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	hcSvc := &failingHealthchecks{}
	base, _ := newApp(t, deadURL, hcSvc)

	status, _, body := do(t, request(t, base, http.MethodGet, "/v2/"))
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", status, http.StatusBadGateway)
	}
	if !strings.Contains(body, "BAD_GATEWAY") {
		t.Fatalf("body = %q, want the docker error payload", body)
	}
	if hcSvc.failures.Load() != 1 {
		t.Fatalf("healthcheck failures = %d, want 1", hcSvc.failures.Load())
	}
}

func TestRouterCachesCatalog(t *testing.T) {
	upstream, hits := newUpstream(t)
	base, _ := newApp(t, upstream.URL, nil)

	status, header, first := do(t, request(t, base, http.MethodGet, "/v2/_catalog"))
	if status != http.StatusOK {
		t.Fatalf("first status = %d, want %d", status, http.StatusOK)
	}
	if cache := header.Get("X-Cache"); cache != "MISS" {
		t.Fatalf("first X-Cache = %q, want MISS", cache)
	}

	status, header, second := do(t, request(t, base, http.MethodGet, "/v2/_catalog"))
	if status != http.StatusOK {
		t.Fatalf("second status = %d, want %d", status, http.StatusOK)
	}
	if cache := header.Get("X-Cache"); cache != "HIT" {
		t.Fatalf("second X-Cache = %q, want HIT", cache)
	}
	if first != second {
		t.Fatalf("cached body = %q, want %q", second, first)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
}

func TestHTTPErrorHandlerKeepsClientErrors(t *testing.T) {
	e := echo.New()
	e.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	e.HTTPErrorHandler = httpErrorHandler
	e.GET("/ok", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })
	base := serve(t, e)

	status, _, _ := do(t, request(t, base, http.MethodGet, "/nope"))
	if status != http.StatusNotFound {
		t.Fatalf("unknown path status = %d, want %d", status, http.StatusNotFound)
	}

	status, _, _ = do(t, request(t, base, http.MethodPost, "/ok"))
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status = %d, want %d", status, http.StatusMethodNotAllowed)
	}
}

// TestHTTPErrorHandlerLogsPanics restores the ERROR record v4's Recover emitted for a panic.
func TestHTTPErrorHandlerLogsPanics(t *testing.T) {
	sink := &logSink{}
	e := echo.New()
	e.Logger = slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelWarn}))
	e.HTTPErrorHandler = httpErrorHandler
	e.Use(middleware.Recover())
	e.GET("/boom", func(*echo.Context) error { panic("kaboom") })
	base := serve(t, e)

	status, _, body := do(t, request(t, base, http.MethodGet, "/boom"))
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
	}
	if strings.Contains(body, "kaboom") {
		t.Fatalf("body = %q, want the panic detail withheld", body)
	}
	if !strings.Contains(sink.String(), "request panicked") {
		t.Fatalf("log = %q, want an ERROR record for the panic", sink.String())
	}
}
