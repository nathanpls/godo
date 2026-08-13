package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsServeMarkdownForAgents(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/http", nil)
	request.Header.Set("Accept", "text/markdown")
	response := httptest.NewRecorder()

	docsHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.HasPrefix(response.Body.String(), "# HTTP\n") {
		t.Fatalf("response does not contain the HTTP Markdown page")
	}
}

func TestDocsServeCLIPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/cli", nil)
	request.Header.Set("Accept", "text/markdown")
	response := httptest.NewRecorder()

	docsHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.HasPrefix(response.Body.String(), "# CLI\n") {
		t.Fatalf("response does not contain the CLI Markdown page")
	}
}

func TestDocsServeORMPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/orm", nil)
	request.Header.Set("Accept", "text/markdown")
	response := httptest.NewRecorder()

	docsHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.HasPrefix(response.Body.String(), "# ORM\n") {
		t.Fatalf("response does not contain the ORM Markdown page")
	}
}

func TestDocsServeRateLimitPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/http/plugins/ratelimit", nil)
	request.Header.Set("Accept", "text/markdown")
	response := httptest.NewRecorder()

	docsHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.HasPrefix(response.Body.String(), "# HTTP Rate Limiting\n") {
		t.Fatalf("response does not contain the rate limiting Markdown page")
	}
}

func TestDocsServeAPIKeyPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/http/plugins/apikey", nil)
	request.Header.Set("Accept", "text/markdown")
	response := httptest.NewRecorder()

	docsHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.HasPrefix(response.Body.String(), "# HTTP API Key Authentication\n") {
		t.Fatalf("response does not contain the API key Markdown page")
	}
}

func TestDocsServeAgentAPIPages(t *testing.T) {
	for _, test := range []struct{ path, prefix string }{
		{"/http/plugins/agentapi", "# HTTP Agent API\n"},
		{"/http/plugins/idempotency", "# HTTP Idempotency\n"},
		{"/http/plugins/requestid", "# HTTP Request IDs\n"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Accept", "text/markdown")
		response := httptest.NewRecorder()
		docsHandler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Body.String(), test.prefix) {
			t.Fatalf("%s returned %d %q", test.path, response.Code, response.Body.String())
		}
	}
}

func TestDocsServeProductionPackagePages(t *testing.T) {
	for _, test := range []struct{ path, prefix string }{
		{"/id", "# IDs\n"},
		{"/lifecycle", "# Service Lifecycle\n"},
		{"/password", "# Password Hashing\n"},
		{"/validate", "# Request Validation\n"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Accept", "text/markdown")
		response := httptest.NewRecorder()
		docsHandler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Body.String(), test.prefix) {
			t.Fatalf("%s returned %d %q", test.path, response.Code, response.Body.String())
		}
	}
}

func TestDocsServeHTMLForBrowsers(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/http", nil)
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	response := httptest.NewRecorder()

	docsHandler().ServeHTTP(response, request)

	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(response.Body.String(), "<!doctype html>") {
		t.Fatalf("response does not contain an HTML page")
	}
}

func TestMarkdownWithZeroQualityFallsBackToHTML(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept", "text/markdown; q=0.0, text/html")
	response := httptest.NewRecorder()

	docsHandler().ServeHTTP(response, request)

	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}
