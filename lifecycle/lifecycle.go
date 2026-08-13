package lifecycle

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"time"
)

// Service is one long-running component of an application.
//
// Run should block until the service stops and observe context cancellation.
// Shutdown may be nil when cancellation alone stops the service.
type Service struct {
	Name     string
	Run      func(context.Context) error
	Shutdown func(context.Context) error
}

type serviceResult struct {
	index int
	err   error
}

// Run starts services concurrently and coordinates their shutdown.
//
// A service stopping or parent cancellation cancels the shared run context and
// invokes every Shutdown function concurrently. Graceful parent cancellation
// and a nil service result are not returned as errors. Runtime errors, shutdown
// errors, and shutdown timeouts are joined in the returned error.
func Run(parent context.Context, shutdownTimeout time.Duration, services ...Service) error {
	if parent == nil {
		return errors.New("lifecycle: parent context must not be nil")
	}
	if shutdownTimeout <= 0 {
		return errors.New("lifecycle: shutdown timeout must be positive")
	}
	if len(services) == 0 {
		return errors.New("lifecycle: at least one service is required")
	}
	names := make(map[string]bool, len(services))
	for _, service := range services {
		if service.Name == "" || service.Run == nil {
			return errors.New("lifecycle: every service requires a name and Run function")
		}
		if names[service.Name] {
			return fmt.Errorf("lifecycle: duplicate service name %q", service.Name)
		}
		names[service.Name] = true
	}

	runContext, cancel := context.WithCancel(parent)
	defer cancel()
	runResults := make(chan serviceResult, len(services))
	for index, service := range services {
		go func() {
			runResults <- serviceResult{index: index, err: service.Run(runContext)}
		}()
	}

	remaining := len(services)
	runErrors := make([]error, len(services))
	for remaining > 0 {
		select {
		case result := <-runResults:
			remaining--
			if !expectedStop(result.err, parent) {
				runErrors[result.index] = fmt.Errorf("lifecycle: %s: %w", services[result.index].Name, result.err)
			}
			cancel()
			return finishShutdown(parent, shutdownTimeout, services, runResults, remaining, runErrors)
		case <-parent.Done():
			cancel()
			return finishShutdown(parent, shutdownTimeout, services, runResults, remaining, runErrors)
		}
	}
	return joinErrors(runErrors)
}

func finishShutdown(parent context.Context, timeout time.Duration, services []Service, runResults <-chan serviceResult, remaining int, runErrors []error) error {
	shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	defer cancel()
	shutdownResults := make(chan serviceResult, len(services))
	shutdownRemaining := 0
	for index, service := range services {
		if service.Shutdown == nil {
			continue
		}
		shutdownRemaining++
		go func() {
			shutdownResults <- serviceResult{index: index, err: service.Shutdown(shutdownContext)}
		}()
	}
	shutdownErrors := make([]error, len(services))
	for remaining > 0 || shutdownRemaining > 0 {
		select {
		case result := <-runResults:
			remaining--
			if !expectedStop(result.err, parent) {
				runErrors[result.index] = fmt.Errorf("lifecycle: %s: %w", services[result.index].Name, result.err)
			}
		case result := <-shutdownResults:
			shutdownRemaining--
			if result.err != nil {
				shutdownErrors[result.index] = fmt.Errorf("lifecycle: shut down %s: %w", services[result.index].Name, result.err)
			}
		case <-shutdownContext.Done():
			return errors.Join(joinErrors(runErrors), joinErrors(shutdownErrors), fmt.Errorf("lifecycle: shutdown timed out: %w", shutdownContext.Err()))
		}
	}
	return errors.Join(joinErrors(runErrors), joinErrors(shutdownErrors))
}

func expectedStop(err error, parent context.Context) bool {
	return err == nil || errors.Is(err, context.Canceled) || parent.Err() != nil && errors.Is(err, parent.Err())
}

func joinErrors(values []error) error {
	var result error
	for _, value := range values {
		result = errors.Join(result, value)
	}
	return result
}

// HTTPServer adapts server into a Service using ListenAndServe and Shutdown.
// If graceful shutdown reaches its deadline, it closes active connections.
func HTTPServer(name string, server *stdhttp.Server) Service {
	return Service{
		Name: name,
		Run: func(context.Context) error {
			if server == nil {
				return errors.New("HTTP server must not be nil")
			}
			err := server.ListenAndServe()
			if errors.Is(err, stdhttp.ErrServerClosed) {
				return nil
			}
			return err
		},
		Shutdown: func(ctx context.Context) error {
			if server == nil {
				return errors.New("HTTP server must not be nil")
			}
			if err := server.Shutdown(ctx); err != nil {
				return errors.Join(err, server.Close())
			}
			return nil
		},
	}
}
