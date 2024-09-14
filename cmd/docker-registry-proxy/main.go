package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/etkecc/go-apm"
	"github.com/etkecc/go-healthchecks/v2"
	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/ziflex/lecho/v3"

	"github.com/etkecc/docker-registry-proxy/internal/config"
	"github.com/etkecc/docker-registry-proxy/internal/controllers"
	"github.com/etkecc/docker-registry-proxy/internal/services"
)

var (
	e   *echo.Echo
	hc  *healthchecks.Client
	log *zerolog.Logger
)

func main() {
	quit := make(chan struct{})

	cfg := config.New()
	apm.SetName("drp")
	// NOTE: due to the goroutine leak in sentry, it's disabled for now
	// ref: https://github.com/getsentry/sentry-go/issues/731
	// apm.SetSentryDSN(cfg.SentryDSN)
	apm.SetLogLevel(cfg.LogLevel)
	apm.WrapClient(nil)
	log = apm.Log()

	log.Info().Msg("#############################")
	log.Info().Msg("Docker Registry Proxy")
	log.Info().Msg("#############################")

	if cfg.Healthchecks.UUID != "" {
		log.Info().Str("url", cfg.Healthchecks.URL).Str("uuid", cfg.Healthchecks.UUID).Msg("Healthchecks enabled")
		hc = healthchecks.New(
			healthchecks.WithBaseURL(cfg.Healthchecks.URL),
			healthchecks.WithCheckUUID(cfg.Healthchecks.UUID),
			healthchecks.WithHTTPClient(apm.WrapClient(nil)),
		)
		apm.SetHealthchecks(hc)
		hc.Start(strings.NewReader("docker-registry-proxy is starting"))
		go hc.Auto(60 * time.Second)
	}

	e = echo.New()
	e.Logger = lecho.From(*log)
	initShutdown(quit)
	defer recovery()
	var authProvider *services.AuthProvider
	if cfg.Allowed.Provider.URL != "" {
		authProvider = services.NewAuthProvider(cfg.Allowed.Provider.URL, cfg.Allowed.Provider.Login, cfg.Allowed.Provider.Password)
	}
	authSvc := services.NewAuth(cfg.Allowed.IPs, cfg.Allowed.UAs, cfg.Trusted.IPs, cfg.Cache.TTL, cfg.Cache.Size, authProvider)
	cacheSvc := services.NewCache(!cfg.Cache.Disabled, cfg.Cache.TTL, cfg.Cache.Size)
	controllers.ConfigureRouter(e, cfg.Metrics, authSvc, cacheSvc, hc, cfg.Target)

	if err := e.Start(":" + cfg.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error().Err(err).Msg("http server failed")
	}

	<-quit
}

func initShutdown(quit chan struct{}) {
	listener := make(chan os.Signal, 1)
	signal.Notify(listener, os.Interrupt, syscall.SIGABRT, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	go func() {
		<-listener
		defer close(quit)

		shutdown(false)
	}()
}

func shutdown(paniced bool) {
	log.Info().Msg("Shutting down...")
	defer sentry.Flush(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		return
	}
	if hc != nil {
		hc.Shutdown()
		if !paniced {
			hc.ExitStatus(0, strings.NewReader("docker-registry-proxy is shutting down"))
		}
	}

	log.Info().Msg("Docker Registry Proxy has been stopped")
	os.Exit(0) //nolint:gocritic // doesn't matter
}

func recovery() {
	err := recover()
	if err != nil {
		defer shutdown(true)
		sentry.CurrentHub().Recover(err)
		if hc != nil {
			hc.ExitStatus(1, strings.NewReader(fmt.Sprintf("panic: %+v", err)))
		}
	} else {
		shutdown(false)
	}
}
