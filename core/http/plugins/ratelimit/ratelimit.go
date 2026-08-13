package ratelimit

import (
	"context"
	"errors"
	"math"
	"net"
	stdhttp "net/http"
	"reflect"
	"strconv"
	"time"

	godohttp "github.com/nathanpls/godo/core/http"
)

const maxKeyBytes = 1024

// Result describes the current fixed window after consuming a request.
type Result struct {
	Allowed   bool
	Remaining int64
	Reset     time.Time
}

// Store atomically consumes one request from a fixed-window limit.
type Store interface {
	Take(context.Context, string, int64, time.Duration, time.Time) (Result, error)
}

// KeyFunc identifies the client or resource being limited.
type KeyFunc func(*stdhttp.Request) string

// Handler handles a denied request.
type Handler func(stdhttp.ResponseWriter, *stdhttp.Request, Result)

// ErrorHandler handles a storage failure.
type ErrorHandler func(stdhttp.ResponseWriter, *stdhttp.Request, error)

// Config configures a fixed-window rate limiter.
type Config struct {
	// Store defaults to a process-local MemoryStore.
	Store Store

	// Limit is the number of allowed requests in each window.
	Limit int64

	// Window is the fixed-window duration.
	Window time.Duration

	// Key identifies a caller. It defaults to the direct remote IP address and
	// deliberately does not trust proxy headers.
	Key KeyFunc

	// Namespace separates unrelated limits that share a Store. It defaults to
	// "default".
	Namespace string

	// Skip bypasses limiting when it returns true.
	Skip func(*stdhttp.Request) bool

	// Denied handles exhausted limits. The default writes HTTP 429.
	Denied Handler

	// OnError handles Store errors. The default fails closed with HTTP 503.
	OnError ErrorHandler
}

// Limiter installs fixed-window rate limiting middleware on a router.
type Limiter struct {
	config Config
	now    func() time.Time
}

// New creates a rate limiting plugin. Configuration is validated by Install.
func New(config Config) *Limiter {
	return &Limiter{config: config, now: time.Now}
}

// Install validates the limiter and adds its middleware to router.
func (limiter *Limiter) Install(router *godohttp.Router) error {
	if limiter == nil {
		return errors.New("ratelimit: limiter must not be nil")
	}
	if router == nil {
		return errors.New("ratelimit: router must not be nil")
	}
	if limiter.config.Limit < 1 || limiter.config.Limit == math.MaxInt64 {
		return errors.New("ratelimit: limit must be between 1 and MaxInt64-1")
	}
	if limiter.config.Window <= 0 {
		return errors.New("ratelimit: window must be greater than zero")
	}
	if limiter.config.Store == nil {
		limiter.config.Store = NewMemoryStore()
	} else if nilInterface(limiter.config.Store) {
		return errors.New("ratelimit: store must not be nil")
	}
	if limiter.config.Key == nil {
		limiter.config.Key = IPKey
	}
	if limiter.config.Namespace == "" {
		limiter.config.Namespace = "default"
	}
	if limiter.config.Denied == nil {
		limiter.config.Denied = defaultDenied
	}
	if limiter.config.OnError == nil {
		limiter.config.OnError = defaultError
	}
	router.Use(limiter.middleware)
	return nil
}

func (limiter *Limiter) middleware(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		if limiter.config.Skip != nil && limiter.config.Skip(request) {
			next.ServeHTTP(w, request)
			return
		}

		now := limiter.now()
		key := strconv.Itoa(len(limiter.config.Namespace)) + ":" + limiter.config.Namespace + limiter.config.Key(request)
		if len(key) > maxKeyBytes {
			limiter.config.OnError(w, request, errors.New("rate limit key is too long"))
			return
		}
		result, err := limiter.config.Store.Take(request.Context(), key, limiter.config.Limit, limiter.config.Window, now)
		if err != nil {
			limiter.config.OnError(w, request, err)
			return
		}
		result.Remaining = max(0, min(result.Remaining, limiter.config.Limit))
		setHeaders(w.Header(), limiter.config.Limit, result, limiter.now())
		if !result.Allowed {
			limiter.config.Denied(w, request, result)
			return
		}
		next.ServeHTTP(w, request)
	})
}

// IPKey identifies a request by its direct remote IP address. Applications
// behind a trusted proxy should provide an explicit Key function that validates
// the proxy and its forwarding header.
func IPKey(request *stdhttp.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func setHeaders(header stdhttp.Header, limit int64, result Result, now time.Time) {
	duration := result.Reset.Sub(now)
	reset := int64(duration / time.Second)
	if duration%time.Second != 0 {
		reset++
	}
	reset = max(int64(0), reset)
	// These legacy fields use a relative reset in seconds.
	header.Set("RateLimit-Limit", strconv.FormatInt(limit, 10))
	header.Set("RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
	header.Set("RateLimit-Reset", strconv.FormatInt(reset, 10))
	if !result.Allowed {
		header.Set("Retry-After", strconv.FormatInt(reset, 10))
	}
}

func defaultDenied(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ Result) {
	_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusTooManyRequests, Title: "Rate limit exceeded"})
}

func defaultError(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ error) {
	_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusServiceUnavailable, Title: "Rate limiter unavailable"})
}

func nilInterface(value any) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
