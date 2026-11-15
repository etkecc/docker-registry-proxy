package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/etkecc/go-apm/v2"
	"github.com/etkecc/go-healthchecks/v2"
	"github.com/etkecc/go-kit/httpclient"
	"github.com/labstack/echo/v5"

	"github.com/etkecc/docker-registry-proxy/internal/config"
	"github.com/etkecc/docker-registry-proxy/internal/controllers"
	"github.com/etkecc/docker-registry-proxy/internal/services"
)

// gracefulTimeout bounds the wait for in-flight requests during shutdown.
const gracefulTimeout = 5 * time.Second

// exitPingTimeout bounds the synchronous healthchecks exit report.
const exitPingTimeout = 5 * time.Second

// failureReporter is the healthcheck surface the router needs, nil when healthchecks are off.
type failureReporter interface {
	Fail(optionalBody ...io.Reader)
}

func main() {
	os.Exit(run())
}

// run starts the service, serves until a signal or a listener failure, and returns the exit code.
func run() (code int) {
	cfg := config.New()
	apm.SetName("drp")
	// sentry stays off until sentry-go#731 is fixed; apm.SetSentryDSN(cfg.SentryDSN) turns it back on.
	apm.SetLogLevel(cfg.LogLevel)
	apm.WrapClient(nil)
	log := apm.Log()
	defer apm.Flush()

	log.Info("#############################")
	log.Info("Docker Registry Proxy")
	log.Info("#############################")

	hc := initHealthchecks(cfg, log)
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Error("panic recovered", "error", fmt.Sprintf("%+v", recovered))
			code = 1
		}
		exitHealthchecks(hc, cfg.Healthchecks, code, log)
	}()

	if err := cfg.Metrics.Validate(); err != nil {
		log.Error("invalid metrics basic auth config", "error", err)
		return 1
	}

	e := echo.New()
	e.Logger = log
	authSvc := services.NewAuth(cfg.Allowed.IPs, cfg.Allowed.UAs, cfg.Trusted.IPs, cfg.Cache.TTL, cfg.Cache.Size, initAuthProvider(cfg))
	cacheSvc := services.NewCache(!cfg.Cache.Disabled, cfg.Cache.TTL, cfg.Cache.Size)
	if err := controllers.ConfigureRouter(e, cfg.Metrics, authSvc, cacheSvc, healthchecksClient(hc), cfg.Target); err != nil {
		log.Error("router configuration failed", "error", err)
		return 1
	}

	serveCtx, cancelServe := context.WithCancel(apm.NewContext())
	defer cancelServe()
	initShutdown(cancelServe, log)

	sc := echo.StartConfig{
		Address:         ":" + cfg.Port,
		HideBanner:      true,
		GracefulTimeout: gracefulTimeout,
		BeforeServeFunc: func(s *http.Server) error {
			// layer uploads stream for minutes; keep the 30s default off the body and bound the headers instead
			s.ReadTimeout = 0
			s.ReadHeaderTimeout = 10 * time.Second
			// net/http internals log at ERROR, which the reporting logger would turn into alerts
			s.ErrorLog = slog.NewLogLogger(slog.NewTextHandler(os.Stderr, nil), slog.LevelError)
			return nil
		},
	}
	if err := sc.Start(serveCtx, e); err != nil {
		log.Error("http server failed", "error", err)
		code = 1
	}
	log.Info("Docker Registry Proxy has been stopped")

	return code
}

// initShutdown cancels the serve context once the process receives a termination signal.
func initShutdown(cancel context.CancelFunc, log *slog.Logger) {
	listener := make(chan os.Signal, 1)
	signal.Notify(listener, os.Interrupt, syscall.SIGABRT, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	go func() {
		sig := <-listener
		log.Info("Shutting down...", "signal", sig.String())
		cancel()
	}()
}

// initHealthchecks starts the healthchecks pings and returns the client, nil when the check is disabled.
func initHealthchecks(cfg *config.Config, log *slog.Logger) *healthchecks.Client {
	if cfg.Healthchecks.UUID == "" {
		return nil
	}

	log.Info("Healthchecks enabled", "url", cfg.Healthchecks.URL, "uuid", cfg.Healthchecks.UUID)
	hc := healthchecks.New(
		healthchecks.WithBaseURL(cfg.Healthchecks.URL),
		healthchecks.WithCheckUUID(cfg.Healthchecks.UUID),
		// own client: wrapping the apm-default one would report ping failures through the pinger itself
		healthchecks.WithHTTPClient(apm.WrapClient(&http.Client{}, apm.WithHealthchecks(false))),
	)
	apm.SetHealthchecks(hc)
	hc.Start(strings.NewReader("docker-registry-proxy is starting"))
	go hc.Auto(60 * time.Second)

	return hc
}

// exitHealthchecks reports the exit status, then drains the pings still in flight.
func exitHealthchecks(hc *healthchecks.Client, cfg config.Healthchecks, code int, log *slog.Logger) {
	if hc == nil {
		return
	}
	reportExit(cfg, code, log)
	hc.Shutdown()
}

// reportExit posts the exit status itself, since the client pings asynchronously and Shutdown cancels them.
func reportExit(cfg config.Healthchecks, code int, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(apm.NewContext(), exitPingTimeout)
	defer cancel()

	endpoint := fmt.Sprintf("%s/%s/%d", strings.TrimSuffix(cfg.URL, "/"), cfg.UUID, code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(exitBody(code)))
	if err != nil {
		log.Warn("healthchecks exit status failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := httpclient.NewSingleHost().Do(req)
	if err != nil {
		log.Warn("healthchecks exit status failed", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Warn("healthchecks exit status rejected", "status", resp.StatusCode)
	}
}

// healthchecksClient keeps a disabled client a nil interface so the router skips failure reporting.
func healthchecksClient(hc *healthchecks.Client) failureReporter {
	if hc == nil {
		return nil
	}
	return hc
}

// exitBody describes the process exit for the healthchecks dashboard.
func exitBody(code int) string {
	if code == 0 {
		return "docker-registry-proxy is shutting down"
	}
	return fmt.Sprintf("docker-registry-proxy exited with code %d", code)
}

// initAuthProvider returns the provider-backed authorizer, nil when no provider is configured.
func initAuthProvider(cfg *config.Config) *services.AuthProvider {
	if cfg.Allowed.Provider.URL == "" {
		return nil
	}
	return services.NewAuthProvider(cfg.Allowed.Provider.URL, cfg.Allowed.Provider.Login, cfg.Allowed.Provider.Password)
}
