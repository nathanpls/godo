package agentapi

import (
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/url"
	"strings"

	godohttp "github.com/nathanpls/godo/core/http"
)

// Authentication describes how agents authenticate.
type Authentication struct {
	Type   string `json:"type"`
	Header string `json:"header,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

// Link is an agent-readable API resource.
type Link struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Manifest is returned by /.well-known/godo.json.
type Manifest struct {
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	Description    string         `json:"description,omitempty"`
	OpenAPI        string         `json:"openapi"`
	Documentation  string         `json:"documentation,omitempty"`
	LLMs           string         `json:"llms"`
	Authentication Authentication `json:"authentication,omitempty"`
}

// Config configures agent-facing discovery endpoints.
type Config struct {
	Name           string
	Version        string
	Description    string
	Documentation  string
	Authentication Authentication
	OpenAPI        *Document
	Links          []Link
}

// Plugin installs agent discovery endpoints.
type Plugin struct{ config Config }

// New creates an agent API discovery plugin.
func New(config Config) *Plugin { return &Plugin{config: config} }

// Install validates and registers discovery routes.
func (plugin *Plugin) Install(router *godohttp.Router) error {
	if plugin == nil || router == nil {
		return errors.New("agentapi: plugin and router must not be nil")
	}
	if plugin.config.Name == "" || len(plugin.config.Name) > 200 || plugin.config.Version == "" || len(plugin.config.Version) > 100 || plugin.config.OpenAPI == nil {
		return errors.New("agentapi: name, version, and OpenAPI document are required")
	}
	if err := plugin.config.OpenAPI.validate(); err != nil {
		return err
	}
	if err := validateAuthentication(plugin.config.Authentication); err != nil {
		return err
	}
	if !safeText(plugin.config.Name) || !safeText(plugin.config.Version) || !safeText(plugin.config.Description) {
		return errors.New("agentapi: metadata must not contain control characters")
	}
	if plugin.config.Documentation != "" && !safePath(plugin.config.Documentation) {
		return errors.New("agentapi: documentation must be an absolute path")
	}
	for _, target := range linkURLs(plugin.config.Links) {
		if !safeLink(target) {
			return errors.New("agentapi: links must be absolute paths or HTTP URLs")
		}
	}
	for _, link := range plugin.config.Links {
		if link.Name == "" || len(link.Name) > 200 || !safeText(link.Name) {
			return errors.New("agentapi: link names must not be empty or contain control characters")
		}
	}
	openAPI, err := json.Marshal(plugin.config.OpenAPI)
	if err != nil {
		return fmt.Errorf("agentapi: encode OpenAPI: %w", err)
	}

	manifest := Manifest{
		Name: plugin.config.Name, Version: plugin.config.Version, Description: plugin.config.Description,
		OpenAPI: "/openapi.json", Documentation: plugin.config.Documentation, LLMs: "/llms.txt",
		Authentication: plugin.config.Authentication,
	}
	router.Get("/.well-known/godo.json", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_ = godohttp.JSON(w, stdhttp.StatusOK, manifest)
	})
	router.Get("/openapi.json", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(openAPI)
	})
	router.Get("/llms.txt", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(plugin.llms()))
	})
	return nil
}

func (plugin *Plugin) llms() string {
	var result strings.Builder
	result.WriteString("# " + plugin.config.Name + "\n\n")
	if plugin.config.Description != "" {
		result.WriteString(plugin.config.Description + "\n\n")
	}
	result.WriteString("- [OpenAPI](/openapi.json): Machine-readable API contract\n")
	if plugin.config.Documentation != "" {
		result.WriteString("- [Documentation](" + plugin.config.Documentation + "): Human and agent documentation\n")
	}
	for _, link := range plugin.config.Links {
		result.WriteString("- [" + markdownLabel(link.Name) + "](" + link.URL + ")\n")
	}
	return result.String()
}

func safeLink(value string) bool {
	if safePath(value) {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil
}

func safePath(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && safeText(value) && !strings.ContainsAny(value, "()\\")
}

func validateAuthentication(authentication Authentication) error {
	switch authentication.Type {
	case "":
		return nil
	case "bearer":
		if !strings.EqualFold(authentication.Header, "Authorization") || !strings.EqualFold(authentication.Scheme, "Bearer") {
			return errors.New("agentapi: bearer authentication requires Authorization and Bearer")
		}
		return nil
	default:
		return fmt.Errorf("agentapi: unsupported authentication type %q", authentication.Type)
	}
}

func safeText(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func markdownLabel(value string) string {
	return strings.NewReplacer("[", "\\[", "]", "\\]").Replace(value)
}

func linkURLs(links []Link) []string {
	result := make([]string, len(links))
	for i, link := range links {
		result[i] = link.URL
	}
	return result
}
