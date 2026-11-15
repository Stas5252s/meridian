package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/meridian-platform/meridian/services/checkout-api/internal/config"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/httpapi"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/obs"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/store"
	"github.com/meridian-platform/meridian/services/checkout-api/internal/version"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := obs.NewLogger(cfg.LogLevel, version.Version, version.Commit)
	metrics := obs.NewMetrics(version.Version, version.Commit)

	var orderStore store.OrderStore
	var memoryStore *store.Memory
	if cfg.RedisAddr != "" {
		client := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		defer func() { _ = client.Close() }()
		orderStore = store.NewRedis(client, cfg.IdempotencyTTL)
		logger.Info("using redis order store", "addr", cfg.RedisAddr, "db", cfg.RedisDB)
	} else {
		memoryStore = store.NewMemory(cfg.IdempotencyTTL)
		orderStore = memoryStore
		logger.Info("using in-memory order store")
	}
	health := httpapi.NewHealth(orderStore)

	handler := httpapi.New(httpapi.Deps{
		Store:        orderStore,
		Metrics:      metrics,
		Logger:       logger,
		Health:       health,
		MaxBodyBytes: cfg.MaxBodyBytes,
		FaultLatency: cfg.FaultLatency,
		Now:          time.Now,
	})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if memoryStore != nil {
		go memoryStore.RunJanitor(ctx, time.Minute)
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", listener.Addr().String(), "build_date", version.BuildDate)
		serveErr <- server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		stop()
		logger.Info("shutdown initiated", "timeout", cfg.ShutdownTimeout.String())
		health.Drain()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		logger.Info("shutdown complete")
		return nil
	}
}
