package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"device-registry/internal/config"
	"device-registry/internal/device"
	"device-registry/internal/httpapi"
	"device-registry/internal/postgres"
	"device-registry/internal/worker"
)

const readHeaderTimeout = 5 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("service stopped with error", "error", err)
		os.Exit(1)
	}
	logger.Info("service stopped")
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create connection pool: %w", err)
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}

	svc := device.NewService(postgres.NewRepository(pool), cfg.MaxDevicesPerOrg, cfg.HeartbeatTimeout, time.Now)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewHandler(svc, logger),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	offlineWorker := worker.NewOfflineWorker(svc, cfg.WorkerInterval, logger)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	group.Go(func() error {
		offlineWorker.Run(groupCtx)
		return nil
	})
	group.Go(func() error {
		<-groupCtx.Done()
		logger.Info("shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})

	return group.Wait()
}
