package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"slices"
	"strings"
	"sync"
	"time"

	godohttp "github.com/nathanpls/godo/http"
)

const (
	defaultTTL              = 24 * time.Hour
	defaultMaxRequestBytes  = 4 << 20
	defaultMaxResponseBytes = 4 << 20
	defaultMaxEntries       = 1_000
	defaultMaxTotalBytes    = 64 << 20
	defaultMaxWaiters       = 100
)

// Config configures retry-safe mutation handling.
type Config struct {
	TTL     time.Duration
	Header  string
	Methods []string
	Require bool

	// Scope isolates equal keys between principals. The default hashes the
	// Authorization header. Cookie or custom authentication must provide Scope.
	Scope func(*stdhttp.Request) string

	MaxRequestBytes  int64
	MaxResponseBytes int64
	MaxEntries       int
	MaxTotalBytes    int64
	MaxWaiters       int
}

// Plugin installs process-local idempotency coordination for buffered,
// non-streaming mutation handlers. Install authentication, authorization, and
// rate limiting before this plugin so they remain outside response replay.
type Plugin struct {
	config Config
	store  *memoryStore
	now    func() time.Time
}

// New creates an idempotency plugin.
func New(config Config) *Plugin { return &Plugin{config: config, now: time.Now} }

// Install validates and installs idempotency middleware.
func (plugin *Plugin) Install(router *godohttp.Router) error {
	if plugin == nil || router == nil {
		return errors.New("idempotency: plugin and router must not be nil")
	}
	if plugin.config.TTL == 0 {
		plugin.config.TTL = defaultTTL
	}
	if plugin.config.TTL < 0 {
		return errors.New("idempotency: TTL must be positive")
	}
	if plugin.config.Header == "" {
		plugin.config.Header = "Idempotency-Key"
	}
	if !validHeaderName(plugin.config.Header) {
		return errors.New("idempotency: invalid header name")
	}
	if len(plugin.config.Methods) == 0 {
		plugin.config.Methods = []string{stdhttp.MethodPost, stdhttp.MethodPut, stdhttp.MethodPatch, stdhttp.MethodDelete}
	}
	for i, method := range plugin.config.Methods {
		method = strings.ToUpper(method)
		if method == "" || strings.ContainsAny(method, "\r\n \t") {
			return fmt.Errorf("idempotency: invalid method %q", method)
		}
		plugin.config.Methods[i] = method
	}
	if plugin.config.Scope == nil {
		plugin.config.Scope = defaultScope
	}
	if plugin.config.MaxRequestBytes == 0 {
		plugin.config.MaxRequestBytes = defaultMaxRequestBytes
	}
	if plugin.config.MaxResponseBytes == 0 {
		plugin.config.MaxResponseBytes = defaultMaxResponseBytes
	}
	if plugin.config.MaxEntries == 0 {
		plugin.config.MaxEntries = defaultMaxEntries
	}
	if plugin.config.MaxTotalBytes == 0 {
		plugin.config.MaxTotalBytes = defaultMaxTotalBytes
	}
	if plugin.config.MaxWaiters == 0 {
		plugin.config.MaxWaiters = defaultMaxWaiters
	}
	if plugin.config.MaxRequestBytes < 1 || plugin.config.MaxResponseBytes < 1 || plugin.config.MaxEntries < 1 || plugin.config.MaxTotalBytes < 1 || plugin.config.MaxWaiters < 1 {
		return errors.New("idempotency: size, entry, and waiter limits must be positive")
	}
	plugin.store = &memoryStore{
		entries: make(map[string]*entry), maxEntries: plugin.config.MaxEntries,
		maxBytes: plugin.config.MaxTotalBytes, maxWaiters: plugin.config.MaxWaiters,
	}
	router.Use(plugin.middleware)
	return nil
}

func (plugin *Plugin) middleware(next stdhttp.Handler) stdhttp.Handler {
	methods := make(map[string]bool, len(plugin.config.Methods))
	for _, method := range plugin.config.Methods {
		methods[method] = true
	}
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		if !methods[request.Method] {
			next.ServeHTTP(w, request)
			return
		}
		values := request.Header.Values(plugin.config.Header)
		if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
			if plugin.config.Require {
				_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusBadRequest, Detail: plugin.config.Header + " is required for this request"})
				return
			}
			next.ServeHTTP(w, request)
			return
		}
		if len(values) != 1 || !validKey(values[0]) {
			_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusBadRequest, Detail: "The idempotency key is invalid"})
			return
		}

		body, err := io.ReadAll(io.LimitReader(request.Body, plugin.config.MaxRequestBytes+1))
		_ = request.Body.Close()
		if err != nil {
			_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusBadRequest, Detail: "The request body could not be read"})
			return
		}
		if int64(len(body)) > plugin.config.MaxRequestBytes {
			_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusRequestEntityTooLarge})
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		fingerprint := requestFingerprint(request, body)
		body = nil
		storageKey := plugin.config.Scope(request) + "\x00" + values[0]
		current, owner, conflict, err := plugin.store.acquire(request.Context(), storageKey, fingerprint, plugin.now())
		if err != nil {
			_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusServiceUnavailable, Detail: "Idempotency coordination is unavailable"})
			return
		}
		if conflict {
			_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusConflict, Detail: "The idempotency key was already used for a different request"})
			return
		}
		if !owner {
			writeCaptured(w, current.response, true)
			return
		}

		response := newCapture(plugin.config.MaxResponseBytes)
		defer func() {
			if recovered := recover(); recovered != nil {
				plugin.store.complete(storageKey, current, terminalResponse("The mutation outcome is unknown"), plugin.config.TTL, plugin.now())
				panic(recovered)
			}
		}()
		next.ServeHTTP(response, request)
		captured := response.result()
		if response.overflow {
			captured = terminalResponse("The mutation completed but its response cannot be replayed")
		}
		plugin.store.complete(storageKey, current, captured, plugin.config.TTL, plugin.now())
		writeCaptured(w, captured, false)
	})
}

func validKey(value string) bool {
	if value == "" || len(value) > 200 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func validHeaderName(value string) bool {
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", character) {
			continue
		}
		return false
	}
	return value != ""
}

func defaultScope(request *stdhttp.Request) string {
	auth := sha256.Sum256([]byte(request.Header.Get("Authorization")))
	return hex.EncodeToString(auth[:])
}

func requestFingerprint(request *stdhttp.Request, body []byte) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, request.Method)
	_, _ = hash.Write([]byte{0})
	_, _ = io.WriteString(hash, request.URL.RequestURI())
	_, _ = hash.Write([]byte{0})
	_, _ = io.WriteString(hash, request.Header.Get("Content-Type"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}

type storedResponse struct {
	status int
	header stdhttp.Header
	body   []byte
}

type entry struct {
	fingerprint string
	expires     time.Time
	done        chan struct{}
	response    storedResponse
	completed   bool
	waiters     int
	size        int64
}

type memoryStore struct {
	mu          sync.Mutex
	entries     map[string]*entry
	maxEntries  int
	maxBytes    int64
	usedBytes   int64
	maxWaiters  int
	nextCleanup time.Time
}

func (store *memoryStore) acquire(ctx context.Context, key, fingerprint string, now time.Time) (*entry, bool, bool, error) {
	for {
		store.mu.Lock()
		if store.nextCleanup.IsZero() || !now.Before(store.nextCleanup) {
			store.cleanup(now)
		}
		current, exists := store.entries[key]
		if !exists {
			if len(store.entries) >= store.maxEntries {
				store.mu.Unlock()
				return nil, false, false, errors.New("idempotency store full")
			}
			current = &entry{fingerprint: fingerprint, done: make(chan struct{})}
			store.entries[key] = current
			store.mu.Unlock()
			return current, true, false, nil
		}
		if current.fingerprint != fingerprint {
			store.mu.Unlock()
			return current, false, true, nil
		}
		if current.completed {
			store.mu.Unlock()
			return current, false, false, nil
		}
		if current.waiters >= store.maxWaiters {
			store.mu.Unlock()
			return nil, false, false, errors.New("too many idempotency waiters")
		}
		current.waiters++
		done := current.done
		store.mu.Unlock()
		select {
		case <-done:
			store.mu.Lock()
			current.waiters--
			store.mu.Unlock()
			continue
		case <-ctx.Done():
			store.mu.Lock()
			current.waiters--
			store.mu.Unlock()
			return nil, false, false, ctx.Err()
		}
	}
}

func (store *memoryStore) cleanup(now time.Time) {
	next := now.Add(time.Minute)
	for key, current := range store.entries {
		if current.completed && !now.Before(current.expires) {
			store.usedBytes -= current.size
			delete(store.entries, key)
			continue
		}
		if current.completed && current.expires.Before(next) {
			next = current.expires
		}
	}
	store.nextCleanup = next
}

func (store *memoryStore) complete(key string, current *entry, response storedResponse, ttl time.Duration, now time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.entries[key] != current || current.completed {
		return
	}
	response.header = replayHeaders(response.header)
	size := int64(len(response.body)) + headerSize(response.header)
	if size > store.maxBytes || store.usedBytes+size > store.maxBytes {
		response = terminalResponse("The mutation completed but its response is unavailable for replay")
		size = int64(len(response.body)) + headerSize(response.header)
		if store.usedBytes+size > store.maxBytes {
			response = storedResponse{status: stdhttp.StatusUnprocessableEntity}
			size = 0
		}
	}
	current.response = response
	current.completed = true
	current.expires = now.Add(ttl)
	current.size = size
	store.usedBytes += size
	if store.nextCleanup.IsZero() || current.expires.Before(store.nextCleanup) {
		store.nextCleanup = current.expires
	}
	close(current.done)
}

func terminalResponse(detail string) storedResponse {
	body := []byte(fmt.Sprintf("{\"type\":\"about:blank\",\"title\":\"Unprocessable Entity\",\"status\":422,\"detail\":%q}\n", detail))
	return storedResponse{status: stdhttp.StatusUnprocessableEntity, header: stdhttp.Header{"Content-Type": {"application/problem+json; charset=utf-8"}}, body: body}
}

func replayHeaders(header stdhttp.Header) stdhttp.Header {
	result := make(stdhttp.Header)
	for name, values := range header {
		switch stdhttp.CanonicalHeaderKey(name) {
		case "Content-Type", "Content-Language", "Content-Encoding", "Content-Disposition", "Location", "Etag", "Cache-Control", "Expires", "Last-Modified", "Vary", "Retry-After":
			result[stdhttp.CanonicalHeaderKey(name)] = slices.Clone(values)
		}
	}
	return result
}

func headerSize(header stdhttp.Header) int64 {
	var size int64
	for name, values := range header {
		size += int64(len(name))
		for _, value := range values {
			size += int64(len(value))
		}
	}
	return size
}

type capture struct {
	header   stdhttp.Header
	status   int
	body     bytes.Buffer
	limit    int64
	overflow bool
}

func newCapture(limit int64) *capture            { return &capture{header: make(stdhttp.Header), limit: limit} }
func (response *capture) Header() stdhttp.Header { return response.header }
func (response *capture) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		return
	}
	if response.status == 0 {
		response.status = status
	}
}
func (response *capture) Write(value []byte) (int, error) {
	if response.status == 0 {
		response.status = stdhttp.StatusOK
	}
	if int64(response.body.Len()+len(value)) > response.limit {
		response.overflow = true
		return 0, errors.New("idempotency: response exceeds capture limit")
	}
	return response.body.Write(value)
}
func (response *capture) result() storedResponse {
	status := response.status
	if status == 0 {
		status = stdhttp.StatusOK
	}
	return storedResponse{status: status, header: response.header.Clone(), body: slices.Clone(response.body.Bytes())}
}

func writeCaptured(w stdhttp.ResponseWriter, response storedResponse, replayed bool) {
	for name, values := range response.header {
		w.Header()[name] = slices.Clone(values)
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.WriteHeader(response.status)
	_, _ = w.Write(response.body)
}
