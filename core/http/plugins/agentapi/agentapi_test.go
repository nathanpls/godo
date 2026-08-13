package agentapi

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	godohttp "github.com/nathanpls/godo/core/http"
)

func TestAgentAPIEndpoints(t *testing.T) {
	document := NewOpenAPI("Plans API", "1.0.0")
	if err := document.AddOperation("POST", "/plans", Operation{
		ID: "createPlan", Summary: "Create a plan",
		Responses: map[string]Response{"201": {Description: "Created"}},
	}); err != nil {
		t.Fatal(err)
	}
	router := godohttp.New()
	if err := router.Install(New(Config{
		Name: "slopdown", Version: "1.0.0", Description: "Share HTML plans",
		Documentation: "/docs", OpenAPI: document,
		Authentication: Authentication{Type: "bearer", Header: "Authorization", Scheme: "Bearer"},
	})); err != nil {
		t.Fatal(err)
	}

	manifestResponse := httptest.NewRecorder()
	router.ServeHTTP(manifestResponse, httptest.NewRequest(stdhttp.MethodGet, "/.well-known/godo.json", nil))
	var manifest Manifest
	if err := json.Unmarshal(manifestResponse.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "slopdown" || manifest.OpenAPI != "/openapi.json" || manifest.LLMs != "/llms.txt" {
		t.Fatalf("manifest = %+v", manifest)
	}

	openAPIResponse := httptest.NewRecorder()
	router.ServeHTTP(openAPIResponse, httptest.NewRequest(stdhttp.MethodGet, "/openapi.json", nil))
	if !strings.Contains(openAPIResponse.Body.String(), `"operationId":"createPlan"`) {
		t.Fatalf("OpenAPI = %s", openAPIResponse.Body.String())
	}

	llmsResponse := httptest.NewRecorder()
	router.ServeHTTP(llmsResponse, httptest.NewRequest(stdhttp.MethodGet, "/llms.txt", nil))
	if llmsResponse.Header().Get("Content-Type") != "text/plain; charset=utf-8" || !strings.Contains(llmsResponse.Body.String(), "[OpenAPI](/openapi.json)") {
		t.Fatalf("llms.txt = %q", llmsResponse.Body.String())
	}
}

func TestOpenAPIValidation(t *testing.T) {
	document := NewOpenAPI("API", "1")
	operation := Operation{ID: "list", Summary: "List", Responses: map[string]Response{"200": {Description: "OK"}}}
	if err := document.AddOperation("GET", "/items", operation); err != nil {
		t.Fatal(err)
	}
	if err := document.AddOperation("GET", "/items", operation); err == nil {
		t.Fatal("duplicate operation was accepted")
	}
	if err := document.AddOperation("QUERY", "/items", operation); err == nil {
		t.Fatal("unsupported OpenAPI method was accepted")
	}
	if err := document.AddSchema("Item", map[string]any{"type": "object"}); err != nil {
		t.Fatal(err)
	}
}

func TestInstallValidatesDirectOpenAPIMutation(t *testing.T) {
	document := NewOpenAPI("API", "1")
	document.Paths["/items"] = map[string]Operation{"made-up": {ID: "invalid"}}
	if err := godohttp.New().Install(New(Config{Name: "api", Version: "1", OpenAPI: document})); err == nil {
		t.Fatal("invalid directly mutated document was accepted")
	}
}
