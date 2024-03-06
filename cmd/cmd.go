package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/ziflex/lecho/v3"
	"gitlab.com/etke.cc/go/apm"
	"gitlab.com/etke.cc/go/psd"

	"gitlab.com/etke.cc/docker-registry-proxy/config"
	"gitlab.com/etke.cc/docker-registry-proxy/controllers"
	"gitlab.com/etke.cc/docker-registry-proxy/services"
)

var (
	e   *echo.Echo
	log *zerolog.Logger
)

func main() {
	quit := make(chan struct{})

	cfg := config.New()
	apm.SetName("drp")
	apm.SetSentryDSN(cfg.SentryDSN)
	apm.SetLogLevel(cfg.LogLevel)
	apm.WrapClient(nil)
	log = apm.Log()

	log.Info().Msg("#############################")
	log.Info().Msg("Docker Registry Proxy")
	log.Info().Msg("#############################")

	e = echo.New()
	e.Logger = lecho.From(*log)
	initShutdown(quit)
	defer recovery()
	var psdc *psd.Client
	if cfg.PSD.URL != "" && cfg.PSD.Login != "" && cfg.PSD.Password != "" {
		psdc = psd.NewClient(cfg.PSD.URL, cfg.PSD.Login, cfg.PSD.Password)
	}
	authSvc := services.NewAuth(cfg.Allowed.IPs, cfg.Allowed.UAs, cfg.Trusted.IPs, cfg.Cache.TTL, cfg.Cache.Size, psdc)
	cacheSvc := services.NewCache(cfg.Cache.TTL, cfg.Cache.Size)
	controllers.ConfigureRouter(e, cfg.Metrics, authSvc, cacheSvc, cfg.Target)

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

		shutdown()
	}()
}

func shutdown() {
	log.Info().Msg("Shutting down...")
	defer sentry.Flush(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		return
	}

	log.Info().Msg("Docker Registry Proxy has been stopped")
	os.Exit(0) //nolint:gocritic // doesn't matter
}

func recovery() {
	defer shutdown()
	err := recover()
	if err != nil {
		sentry.CurrentHub().Recover(err)
	}
}
