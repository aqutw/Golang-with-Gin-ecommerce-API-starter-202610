package main

import (
	"context"
	"errors"
	"example.com/ecommerce/api/internal/httpapi"
	"example.com/ecommerce/commerce/adapters/sqlite"
	catalogapp "example.com/ecommerce/commerce/application/catalog"
	"example.com/ecommerce/commerce/application/checkout"
	"example.com/ecommerce/commerce/domain/discount"
	"example.com/ecommerce/commerce/domain/pricing"
	"example.com/ecommerce/platform/config"
	"example.com/ecommerce/platform/httpx"
	"fmt"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Environment == "production" {
		return fmt.Errorf("static demo identity and automatic demo seed are not supported in production; replace identity and startup policy first")
	}
	gin.SetMode(gin.ReleaseMode)
	// This composition root is the only place choosing concrete adapters.
	initCtx, cancelInit := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelInit()
	store, err := sqlite.Open(initCtx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(initCtx); err != nil {
		return fmt.Errorf("migrate SQLite: %w", err)
	}
	if err := store.Seed(initCtx); err != nil {
		return fmt.Errorf("seed SQLite: %w", err)
	}
	handler := httpapi.New(httpapi.Dependencies{Catalog: catalogapp.Service{Store: store},
		Checkout: checkout.Service{Store: store, Pricing: pricing.Policy{Discounts: discount.NewEngine(nil)}, NewID: httpx.NewID, Now: time.Now}, Config: cfg, Logger: logger, Ready: store.Ping, Storage: "sqlite"})
	server := &http.Server{Addr: cfg.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	logger.Info("server started", "address", cfg.Address, "environment", cfg.Environment, "storage", "sqlite", "database_path", cfg.DatabasePath)
	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
