package requestid

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	godohttp "github.com/nathanpls/godo/http"
)

func TestRequestID(t *testing.T) {
	router := godohttp.New()
	plugin := New(Config{Generate: func() (string, error) { return "req_generated", nil }})
	if err := router.Install(plugin); err != nil {
		t.Fatal(err)
	}
	router.Get("/", func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		id, ok := FromContext(request.Context())
		if !ok || id != "req_generated" {
			t.Errorf("request ID = %q, ok = %t", id, ok)
		}
		w.WriteHeader(stdhttp.StatusNoContent)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, "/", nil))
	if got := response.Header().Get("X-Request-ID"); got != "req_generated" {
		t.Fatalf("X-Request-ID = %q", got)
	}
}

func TestIncomingRequestIDMustBeTrusted(t *testing.T) {
	for _, trust := range []bool{false, true} {
		router := godohttp.New()
		plugin := New(Config{
			Generate: func() (string, error) { return "generated", nil },
			Accept:   func(*stdhttp.Request, string) bool { return trust },
		})
		if err := router.Install(plugin); err != nil {
			t.Fatal(err)
		}
		router.Get("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) { w.WriteHeader(stdhttp.StatusNoContent) })
		request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
		request.Header.Set("X-Request-ID", "incoming")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := "generated"
		if trust {
			want = "incoming"
		}
		if got := response.Header().Get("X-Request-ID"); got != want {
			t.Fatalf("trust %t: ID = %q, want %q", trust, got, want)
		}
	}
}

func TestDuplicateIncomingRequestIDsAreReplaced(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{
		Generate: func() (string, error) { return "generated", nil },
		Accept:   func(*stdhttp.Request, string) bool { return true },
	})); err != nil {
		t.Fatal(err)
	}
	router.Get("/", func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		if got := request.Header.Values("X-Request-ID"); len(got) != 1 || got[0] != "generated" {
			t.Errorf("request header = %v", got)
		}
	})
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	request.Header.Add("X-Request-ID", "one")
	request.Header.Add("X-Request-ID", "two")
	router.ServeHTTP(httptest.NewRecorder(), request)
}

func TestRequestIDGenerationError(t *testing.T) {
	router := godohttp.New()
	if err := router.Install(New(Config{Generate: func() (string, error) { return "", errors.New("entropy failed") }})); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, "/", nil))
	if response.Code != stdhttp.StatusServiceUnavailable || response.Header().Get("Content-Type") != "application/problem+json; charset=utf-8" {
		t.Fatalf("status = %d, Content-Type = %q", response.Code, response.Header().Get("Content-Type"))
	}
}
