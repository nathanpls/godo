package lifecycle

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunCoordinatesCancellationAndShutdown(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	var stopped, shutDown atomic.Bool
	service := Service{
		Name: "worker",
		Run: func(ctx context.Context) error {
			<-ctx.Done()
			stopped.Store(true)
			return ctx.Err()
		},
		Shutdown: func(context.Context) error {
			shutDown.Store(true)
			return nil
		},
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	if err := Run(parent, time.Second, service); err != nil {
		t.Fatal(err)
	}
	if !stopped.Load() || !shutDown.Load() {
		t.Fatalf("stopped = %t, shutdown = %t", stopped.Load(), shutDown.Load())
	}
}

func TestRunReturnsRuntimeAndShutdownErrors(t *testing.T) {
	runErr := errors.New("run failed")
	shutdownErr := errors.New("shutdown failed")
	err := Run(context.Background(), time.Second,
		Service{Name: "api", Run: func(context.Context) error { return runErr }, Shutdown: func(context.Context) error { return shutdownErr }},
		Service{Name: "worker", Run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }},
	)
	if !errors.Is(err, runErr) || !errors.Is(err, shutdownErr) || !strings.Contains(err.Error(), "api") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunBoundsShutdown(t *testing.T) {
	err := Run(context.Background(), 10*time.Millisecond, Service{
		Name: "stuck",
		Run:  func(context.Context) error { return nil },
		Shutdown: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunValidation(t *testing.T) {
	if err := Run(nil, time.Second, Service{}); err == nil {
		t.Fatal("nil context was accepted")
	}
	if err := Run(context.Background(), 0, Service{}); err == nil {
		t.Fatal("zero timeout was accepted")
	}
	if err := Run(context.Background(), time.Second); err == nil {
		t.Fatal("empty services were accepted")
	}
}
