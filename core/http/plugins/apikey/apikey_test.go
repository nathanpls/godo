package apikey

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	godohttp "github.com/nathanpls/godo/core/http"
)

type staticStore struct {
	identity Key
	token    string
	err      error
}

func (store staticStore) Authenticate(token string) (Key, bool, error) {
	return store.identity, token == store.token, store.err
}

type nilStore struct{}

func (*nilStore) Authenticate(string) (Key, bool, error) { return Key{}, false, nil }

func TestPluginAuthenticatesBearerKey(t *testing.T) {
	router := godohttp.New()
	identity := Key{ID: 3, Name: "agent", Prefix: "godo_abcd"}
	if err := router.Install(New(Config{Store: staticStore{identity: identity, token: "secret"}})); err != nil {
		t.Fatal(err)
	}
	router.Get("/", func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		got, ok := KeyFromContext(request.Context())
		if !ok || got.ID != identity.ID {
			t.Errorf("identity = %+v, ok = %t", got, ok)
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer secret")
	router.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, stdhttp.StatusNoContent)
	}
}

func TestPluginRejectsInvalidAuthorization(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{Store: staticStore{token: "secret"}, Realm: "slopdown"})); err != nil {
		t.Fatal(err)
	}
	router.Get("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(stdhttp.StatusNoContent) })

	for _, authorization := range []string{"", "Basic secret", "Bearer", "Bearer wrong", "Bearer secret extra"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
		request.Header.Set("Authorization", authorization)
		router.ServeHTTP(response, request)
		if response.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("authorization %q status = %d", authorization, response.Code)
		}
		if got := response.Header().Get("WWW-Authenticate"); got != `Bearer realm="slopdown"` {
			t.Fatalf("WWW-Authenticate = %q", got)
		}
	}
}

func TestPluginRejectsDuplicateAuthorization(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{Store: staticStore{token: "secret"}})); err != nil {
		t.Fatal(err)
	}
	router.Get("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(stdhttp.StatusNoContent) })
	response := httptest.NewRecorder()
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.Header.Add("Authorization", "Bearer secret")
	request.Header.Add("Authorization", "Bearer other")
	router.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, stdhttp.StatusUnauthorized)
	}
}

func TestPluginSkipAndStoreError(t *testing.T) {
	storeErr := errors.New("disk failed")
	router := godohttp.New()
	if err := router.Install(New(Config{
		Store: staticStore{token: "secret", err: storeErr},
		Skip:  func(request *stdhttp.Request) bool { return request.URL.Path == "/health" },
	})); err != nil {
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
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer secret")
	router.ServeHTTP(failed, request)
	if failed.Code != stdhttp.StatusServiceUnavailable || failed.Header().Get("Content-Type") != "application/problem+json; charset=utf-8" {
		t.Fatalf("failure = %d %q", failed.Code, failed.Body.String())
	}
}

func TestPluginValidation(t *testing.T) {
	if err := godohttp.New().Install(New(Config{})); err == nil {
		t.Fatal("missing store was accepted")
	}
	var store *nilStore
	if err := godohttp.New().Install(New(Config{Store: store})); err == nil {
		t.Fatal("typed nil store was accepted")
	}
	if err := godohttp.New().Install(New(Config{Store: staticStore{}, Realm: "bad\nrealm"})); err == nil {
		t.Fatal("invalid realm was accepted")
	}
	if err := godohttp.New().Install(New(Config{Store: staticStore{}, Realm: `bad\realm`})); err == nil {
		t.Fatal("realm with backslash was accepted")
	}
}

func TestRequireScopes(t *testing.T) {
	middleware, err := Require("plans:write")
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusNoContent)
	}))
	requestWithKey := func(scopes ...string) *stdhttp.Request {
		request := httptest.NewRequest(stdhttp.MethodPost, "/plans", nil)
		return request.WithContext(context.WithValue(request.Context(), identityContextKey{}, Key{Scopes: scopes}))
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, requestWithKey("plans:read"))
	if response.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, requestWithKey("plans:write"))
	if response.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
}
