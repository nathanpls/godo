package ratelimit

import (
	"context"
	"errors"
	"math"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	godohttp "github.com/nathanpls/godo/http"
)

type errorStore struct{ err error }

func (store errorStore) Take(context.Context, string, int64, time.Duration, time.Time) (Result, error) {
	return Result{}, store.err
}

type nilStore struct{}

func (*nilStore) Take(context.Context, string, int64, time.Duration, time.Time) (Result, error) {
	return Result{}, nil
}

func TestLimiter(t *testing.T) {
	start := time.Unix(100, 0)
	limiter := New(Config{
		Limit:     2,
		Window:    time.Minute,
		Key:       func(*stdhttp.Request) string { return "client" },
		Namespace: "api",
	})
	limiter.now = func() time.Time { return start }
	router := godohttp.New()
	if err := router.Install(limiter); err != nil {
		t.Fatal(err)
	}
	router.Get("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusNoContent)
	})

	wantStatus := []int{stdhttp.StatusNoContent, stdhttp.StatusNoContent, stdhttp.StatusTooManyRequests}
	wantRemaining := []string{"1", "0", "0"}
	for i := range wantStatus {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
		router.ServeHTTP(response, request)
		if response.Code != wantStatus[i] {
			t.Fatalf("request %d status = %d, want %d", i+1, response.Code, wantStatus[i])
		}
		if got := response.Header().Get("RateLimit-Limit"); got != "2" {
			t.Fatalf("RateLimit-Limit = %q", got)
		}
		if got := response.Header().Get("RateLimit-Remaining"); got != wantRemaining[i] {
			t.Fatalf("request %d remaining = %q, want %q", i+1, got, wantRemaining[i])
		}
		if got := response.Header().Get("RateLimit-Reset"); got != "60" {
			t.Fatalf("RateLimit-Reset = %q", got)
		}
	}
}

func TestLimiterResetsWindow(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := New(Config{Limit: 1, Window: time.Second, Key: func(*stdhttp.Request) string { return "client" }})
	limiter.now = func() time.Time { return now }
	router := godohttp.New()
	if err := router.Install(limiter); err != nil {
		t.Fatal(err)
	}
	router.Get("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(stdhttp.StatusNoContent) })

	request := func() int {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, "/", nil))
		return response.Code
	}
	if got := request(); got != stdhttp.StatusNoContent {
		t.Fatalf("first status = %d", got)
	}
	if got := request(); got != stdhttp.StatusTooManyRequests {
		t.Fatalf("limited status = %d", got)
	}
	now = now.Add(time.Second)
	if got := request(); got != stdhttp.StatusNoContent {
		t.Fatalf("reset status = %d", got)
	}
}

func TestLimiterSkipAndError(t *testing.T) {
	storeErr := errors.New("database unavailable")
	limiter := New(Config{
		Store:  errorStore{err: storeErr},
		Limit:  1,
		Window: time.Minute,
		Skip:   func(request *stdhttp.Request) bool { return request.URL.Path == "/health" },
	})
	router := godohttp.New()
	if err := router.Install(limiter); err != nil {
		t.Fatal(err)
	}
	router.Get("/health", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(stdhttp.StatusNoContent) })
	router.Get("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(stdhttp.StatusNoContent) })

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(stdhttp.MethodGet, "/health", nil))
	if health.Code != stdhttp.StatusNoContent {
		t.Fatalf("health status = %d", health.Code)
	}

	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, httptest.NewRequest(stdhttp.MethodGet, "/", nil))
	if failed.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("failure status = %d", failed.Code)
	}
	if got := failed.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Fatalf("failure Content-Type = %q", got)
	}
}

func TestLimiterValidation(t *testing.T) {
	tests := []Config{
		{Limit: 0, Window: time.Second},
		{Limit: math.MaxInt64, Window: time.Second},
		{Limit: 1, Window: 0},
	}
	for _, config := range tests {
		if err := godohttp.New().Install(New(config)); err == nil {
			t.Fatalf("invalid config was accepted: %+v", config)
		}
	}
	var store *nilStore
	if err := godohttp.New().Install(New(Config{Store: store, Limit: 1, Window: time.Second})); err == nil {
		t.Fatal("typed nil store was accepted")
	}
}

func TestLimiterRejectsLongKey(t *testing.T) {
	limiter := New(Config{
		Limit: 1, Window: time.Minute,
		Key: func(*stdhttp.Request) string { return string(make([]byte, maxKeyBytes+1)) },
	})
	router := godohttp.New()
	if err := router.Install(limiter); err != nil {
		t.Fatal(err)
	}
	router.Get("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(stdhttp.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, "/", nil))
	if response.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, stdhttp.StatusServiceUnavailable)
	}
}

func TestZeroValueMemoryStore(t *testing.T) {
	store := &MemoryStore{}
	result, err := store.Take(t.Context(), "key", 1, time.Second, time.Now())
	if err != nil || !result.Allowed {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	var nilStore *MemoryStore
	if _, err := nilStore.Take(t.Context(), "key", 1, time.Second, time.Now()); err == nil {
		t.Fatal("nil memory store succeeded")
	}
}

func TestMemoryStoreKeyLimit(t *testing.T) {
	store := &MemoryStore{maxKeys: 1}
	now := time.Now()
	if _, err := store.Take(t.Context(), "one", 1, time.Minute, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Take(t.Context(), "two", 1, time.Minute, now); !errors.Is(err, errMemoryStoreFull) {
		t.Fatalf("error = %v, want %v", err, errMemoryStoreFull)
	}
	if _, err := store.Take(t.Context(), "two", 1, time.Minute, now.Add(time.Minute)); err != nil {
		t.Fatalf("expired key was not cleaned: %v", err)
	}
}

func TestMemoryStoreConcurrentLimit(t *testing.T) {
	store := NewMemoryStore()
	const limit int64 = 100
	const requests = 500
	now := time.Now()
	var allowed int64
	var mutex sync.Mutex
	var wait sync.WaitGroup
	for range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := store.Take(context.Background(), "client", limit, time.Minute, now)
			if err != nil {
				t.Error(err)
				return
			}
			if result.Allowed {
				mutex.Lock()
				allowed++
				mutex.Unlock()
			}
		}()
	}
	wait.Wait()
	if allowed != limit {
		t.Fatalf("allowed = %d, want %d", allowed, limit)
	}
}

func TestIPKey(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.1")
	if got := IPKey(request); got != "192.0.2.1" {
		t.Fatalf("IPKey = %q", got)
	}
	request.RemoteAddr = "local-client"
	if got := IPKey(request); got != "local-client" {
		t.Fatalf("IPKey without port = %q", got)
	}
}

func TestSetHeadersRoundsResetUp(t *testing.T) {
	now := time.Unix(100, 0)
	header := make(stdhttp.Header)
	setHeaders(header, 5, Result{Allowed: false, Remaining: 0, Reset: now.Add(1500 * time.Millisecond)}, now)
	if got := header.Get("RateLimit-Reset"); got != strconv.Itoa(2) {
		t.Fatalf("RateLimit-Reset = %q", got)
	}
	if got := header.Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q", got)
	}
}
