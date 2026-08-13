package idempotency

import (
	"bytes"
	stdhttp "net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	godohttp "github.com/nathanpls/godo/core/http"
)

func TestIdempotencyReplay(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{Require: true})); err != nil {
		t.Fatal(err)
	}
	var executions atomic.Int64
	router.Post("/plans", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		executions.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stdhttp.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"one"}`))
	})

	request := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		r := httptest.NewRequest(stdhttp.MethodPost, "/plans", bytes.NewBufferString(body))
		r.Header.Set("Idempotency-Key", "agent-request-1")
		router.ServeHTTP(response, r)
		return response
	}
	first := request("same")
	second := request("same")
	if first.Code != stdhttp.StatusCreated || second.Code != stdhttp.StatusCreated || first.Body.String() != second.Body.String() {
		t.Fatalf("responses = %d %q, %d %q", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	if executions.Load() != 1 || second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("executions = %d, replay = %q", executions.Load(), second.Header().Get("Idempotency-Replayed"))
	}
	conflict := request("different")
	if conflict.Code != stdhttp.StatusConflict {
		t.Fatalf("conflict status = %d", conflict.Code)
	}
}

func TestConcurrentRequestsShareExecution(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{})); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var executions atomic.Int64
	router.Post("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		if executions.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = w.Write([]byte("ok"))
	})

	responses := make([]*httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for i := range responses {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			responses[i] = httptest.NewRecorder()
			request := httptest.NewRequest(stdhttp.MethodPost, "/", bytes.NewBufferString("same"))
			request.Header.Set("Idempotency-Key", "same")
			router.ServeHTTP(responses[i], request)
		}(i)
	}
	<-started
	close(release)
	wait.Wait()
	if executions.Load() != 1 || responses[0].Body.String() != "ok" || responses[1].Body.String() != "ok" {
		t.Fatalf("executions = %d, bodies = %q %q", executions.Load(), responses[0].Body.String(), responses[1].Body.String())
	}
}

func TestRequiredKeyAndServerErrors(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{Require: true})); err != nil {
		t.Fatal(err)
	}
	var executions int
	router.Post("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		executions++
		w.WriteHeader(stdhttp.StatusInternalServerError)
	})
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(stdhttp.MethodPost, "/", nil))
	if missing.Code != stdhttp.StatusBadRequest {
		t.Fatalf("missing status = %d", missing.Code)
	}
	for range 2 {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
		request.Header.Set("Idempotency-Key", "retry")
		router.ServeHTTP(response, request)
	}
	if executions != 1 {
		t.Fatalf("server error was not cached; executions = %d", executions)
	}
}

func TestWaiterReplaysServerError(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{})); err != nil {
		t.Fatal(err)
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var executions atomic.Int64
	router.Post("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		if executions.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
			w.WriteHeader(stdhttp.StatusInternalServerError)
			return
		}
	})

	responses := make([]*httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for i := range responses {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			responses[i] = httptest.NewRecorder()
			request := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
			request.Header.Set("Idempotency-Key", "retry")
			router.ServeHTTP(responses[i], request)
		}(i)
	}
	<-firstStarted
	close(releaseFirst)
	wait.Wait()
	if executions.Load() != 1 {
		t.Fatalf("executions = %d, want 1", executions.Load())
	}
	if responses[0].Code != 500 || responses[1].Code != 500 {
		t.Fatalf("statuses = %d %d", responses[0].Code, responses[1].Code)
	}
}

func TestPanicCreatesTerminalReplay(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{})); err != nil {
		t.Fatal(err)
	}
	var executions int
	router.Post("/", func(stdhttp.ResponseWriter, *stdhttp.Request) {
		executions++
		panic("failed after mutation")
	})
	request := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		r := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
		r.Header.Set("Idempotency-Key", "panic")
		func() {
			defer func() { _ = recover() }()
			router.ServeHTTP(response, r)
		}()
		return response
	}
	request()
	replay := request()
	if executions != 1 || replay.Code != stdhttp.StatusUnprocessableEntity || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("executions = %d, replay = %d %q", executions, replay.Code, replay.Header().Get("Idempotency-Replayed"))
	}
}

func TestReplayDropsSensitiveAndRequestHeaders(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{})); err != nil {
		t.Fatal(err)
	}
	router.Post("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "secret=value")
		w.Header().Set("X-Request-ID", "old")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	for attempt := range 2 {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(stdhttp.MethodPost, "/", nil)
		request.Header.Set("Idempotency-Key", "headers")
		router.ServeHTTP(response, request)
		if attempt == 1 && (response.Header().Get("Set-Cookie") != "" || response.Header().Get("X-Request-ID") != "" || response.Header().Get("Content-Type") != "application/json") {
			t.Fatalf("replay headers = %v", response.Header())
		}
	}
}

func TestCompletedEntryExpiresAtTTL(t *testing.T) {
	now := time.Unix(1_000, 0)
	plugin := New(Config{TTL: time.Second})
	plugin.now = func() time.Time { return now }
	router := godohttp.New()
	if err := router.Install(plugin); err != nil {
		t.Fatal(err)
	}
	var executions int
	router.Post("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		executions++
		_, _ = w.Write([]byte("ok"))
	})
	request := func(body string) int {
		response := httptest.NewRecorder()
		r := httptest.NewRequest(stdhttp.MethodPost, "/", bytes.NewBufferString(body))
		r.Header.Set("Idempotency-Key", "expires")
		router.ServeHTTP(response, r)
		return response.Code
	}
	request("first")
	now = now.Add(2 * time.Second)
	if status := request("second"); status != stdhttp.StatusOK || executions != 2 {
		t.Fatalf("status = %d, executions = %d", status, executions)
	}
}
