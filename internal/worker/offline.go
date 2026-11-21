package worker

import (
	"context"
	"log/slog"
	"time"
)

type StaleDeviceMarker interface {
	MarkStaleOffline(ctx context.Context) (int64, error)
}

type OfflineWorker struct {
	marker   StaleDeviceMarker
	interval time.Duration
	logger   *slog.Logger
}

func NewOfflineWorker(marker StaleDeviceMarker, interval time.Duration, logger *slog.Logger) *OfflineWorker {
	return &OfflineWorker{marker: marker, interval: interval, logger: logger}
}

func (w *OfflineWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.markStale(ctx)
		}
	}
}

func (w *OfflineWorker) markStale(ctx context.Context) {
	n, err := w.marker.MarkStaleOffline(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("mark stale devices offline", "error", err)
		}
		return
	}
	if n > 0 {
		w.logger.Info("marked devices offline", "count", n)
	}
}
