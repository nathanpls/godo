package main

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPICheck(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		w.Header().Set("X-Request-ID", "req_test")
		switch request.URL.Path {
		case "/.well-known/godo.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"test","version":"1","openapi":"/openapi.json","documentation":"/docs","llms":"/llms.txt","authentication":{"type":"bearer","header":"Authorization","scheme":"Bearer"}}`))
		case "/openapi.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"openapi":"3.1.0","info":{"title":"Test","version":"1"},"paths":{"/items":{}}}`))
		case "/llms.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# API\n"))
		case "/docs":
			if request.Header.Get("Accept") != "text/markdown" {
				t.Error("documentation did not request Markdown")
			}
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte("# Docs\n"))
		default:
			stdhttp.NotFound(w, request)
		}
	}))
	defer server.Close()

	var output strings.Builder
	application := &app{stdout: &output}
	if err := application.run([]string{"api", "check", server.URL}); err != nil {
		t.Fatal(err)
	}
	for _, check := range []string{"PASS discovery", "PASS request IDs", "PASS OpenAPI", "PASS llms.txt", "PASS Markdown", "PASS bearer"} {
		if !strings.Contains(output.String(), check) {
			t.Fatalf("output does not contain %q:\n%s", check, output.String())
		}
	}
}

func TestAPICheckRejectsInvalidURL(t *testing.T) {
	application := &app{stdout: &strings.Builder{}}
	if err := application.run([]string{"api", "check", "file:///tmp/api"}); err == nil {
		t.Fatal("file URL was accepted")
	}
}

func TestAPICheckRejectsCrossOriginManifestLink(t *testing.T) {
	other := httptest.NewServer(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {
		t.Fatal("cross-origin target was requested")
	}))
	defer other.Close()
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"test","version":"1","openapi":"` + other.URL + `/openapi.json","llms":"/llms.txt"}`))
	}))
	defer server.Close()
	application := &app{stdout: &strings.Builder{}}
	if err := application.run([]string{"api", "check", server.URL}); err == nil {
		t.Fatal("cross-origin manifest URL was accepted")
	}
}

func TestAPICheckRejectsCrossOriginRedirect(t *testing.T) {
	requested := false
	other := httptest.NewServer(stdhttp.HandlerFunc(func(stdhttp.ResponseWriter, *stdhttp.Request) {
		requested = true
	}))
	defer other.Close()
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		if request.URL.Path == "/.well-known/godo.json" {
			stdhttp.Redirect(w, request, other.URL, stdhttp.StatusFound)
		}
	}))
	defer server.Close()
	application := &app{stdout: &strings.Builder{}}
	if err := application.run([]string{"api", "check", server.URL}); err == nil {
		t.Fatal("cross-origin redirect was accepted")
	}
	if requested {
		t.Fatal("cross-origin redirect target was requested")
	}
}
