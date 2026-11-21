package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

const (
	testInterval = time.Millisecond
	testTimeout  = 2 * time.Second
)

type stubMarker struct {
	calls chan struct{}
	err   error
}

func newStubMarker(err error) *stubMarker {
	return &stubMarker{calls: make(chan struct{}, 1000), err: err}
}

func (m *stubMarker) MarkStaleOffline(context.Context) (int64, error) {
	select {
	case m.calls <- struct{}{}:
	default:
	}
	return 0, m.err
}

func startWorker(t *testing.T, marker *stubMarker) (stop func(), done <-chan struct{}) {
	t.Helper()

	w := NewOfflineWorker(marker, testInterval, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		w.Run(ctx)
	}()
	t.Cleanup(cancel)
	return cancel, finished
}

func waitForCalls(t *testing.T, marker *stubMarker, n int) {
	t.Helper()

	timeout := time.After(testTimeout)
	for range n {
		select {
		case <-marker.calls:
		case <-timeout:
			t.Fatalf("worker made fewer than %d calls within %v", n, testTimeout)
		}
	}
}

func waitUntilDone(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("worker did not stop after context cancellation")
	}
}

func TestWorkerMarksStaleDevicesRepeatedly(t *testing.T) {
	marker := newStubMarker(nil)

	stop, done := startWorker(t, marker)
	waitForCalls(t, marker, 3)

	stop()
	waitUntilDone(t, done)
}

func TestWorkerKeepsRunningAfterErrors(t *testing.T) {
	marker := newStubMarker(errors.New("database unavailable"))

	stop, done := startWorker(t, marker)
	waitForCalls(t, marker, 3)

	select {
	case <-done:
		t.Fatal("worker stopped after an error")
	default:
	}
	stop()
	waitUntilDone(t, done)
}

func TestWorkerStopsWhenContextIsAlreadyCancelled(t *testing.T) {
	marker := newStubMarker(nil)
	w := NewOfflineWorker(marker, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	waitUntilDone(t, done)
}
