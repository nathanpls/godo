package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestRouterMethods(t *testing.T) {
	methods := []struct {
		name     string
		register func(*Router, string, stdhttp.HandlerFunc)
	}{
		{stdhttp.MethodConnect, (*Router).Connect},
		{stdhttp.MethodDelete, (*Router).Delete},
		{stdhttp.MethodGet, (*Router).Get},
		{stdhttp.MethodHead, (*Router).Head},
		{stdhttp.MethodOptions, (*Router).Options},
		{stdhttp.MethodPatch, (*Router).Patch},
		{stdhttp.MethodPost, (*Router).Post},
		{stdhttp.MethodPut, (*Router).Put},
		{"QUERY", (*Router).Query},
		{stdhttp.MethodTrace, (*Router).Trace},
	}

	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			router := New()
			method.register(router, "/resources/{id}", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.Header().Set("Resource-ID", r.PathValue("id"))
				w.WriteHeader(stdhttp.StatusNoContent)
			})

			request := httptest.NewRequest(method.name, "/resources/42", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != stdhttp.StatusNoContent {
				t.Fatalf("status = %d, want %d", response.Code, stdhttp.StatusNoContent)
			}
			if got := response.Header().Get("Resource-ID"); got != "42" {
				t.Fatalf("Resource-ID = %q, want %q", got, "42")
			}
		})
	}
}

func TestRouterMiddlewareOrder(t *testing.T) {
	router := New()
	var calls []string

	middleware := func(name string) Middleware {
		return func(next stdhttp.Handler) stdhttp.Handler {
			return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				calls = append(calls, name+" before")
				next.ServeHTTP(w, r)
				calls = append(calls, name+" after")
			})
		}
	}

	router.Use(middleware("first"), middleware("second"))
	router.Get("/", func(stdhttp.ResponseWriter, *stdhttp.Request) {
		calls = append(calls, "handler")
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(stdhttp.MethodGet, "/", nil))

	want := []string{"first before", "second before", "handler", "second after", "first after"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestJSON(t *testing.T) {
	response := httptest.NewRecorder()
	if err := JSON(response, stdhttp.StatusCreated, map[string]string{"status": "created"}); err != nil {
		t.Fatal(err)
	}

	if response.Code != stdhttp.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, stdhttp.StatusCreated)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Body.String(); got != "{\"status\":\"created\"}\n" {
		t.Fatalf("body = %q", got)
	}
}
