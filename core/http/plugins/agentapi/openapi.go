package agentapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Info is OpenAPI service metadata.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// Document is an OpenAPI 3.1 document with explicit operations and schemas.
type Document struct {
	OpenAPI    string                          `json:"openapi"`
	Info       Info                            `json:"info"`
	Paths      map[string]map[string]Operation `json:"paths"`
	Components Components                      `json:"components,omitempty"`
}

// Components contains reusable OpenAPI schemas and security schemes.
type Components struct {
	Schemas         map[string]any            `json:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

// SecurityScheme is an OpenAPI security scheme.
type SecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

// Operation explicitly describes an HTTP operation.
type Operation struct {
	ID          string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Description string                `json:"description,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

// Parameter is an OpenAPI operation parameter.
type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Schema      any    `json:"schema"`
}

// RequestBody is an OpenAPI request body.
type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Content     map[string]MediaType `json:"content"`
}

// Response is an OpenAPI response.
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// MediaType describes a request or response schema.
type MediaType struct {
	Schema any `json:"schema,omitempty"`
}

// NewOpenAPI creates an empty OpenAPI 3.1 document.
func NewOpenAPI(title, version string) *Document {
	return &Document{
		OpenAPI:    "3.1.0",
		Info:       Info{Title: title, Version: version},
		Paths:      make(map[string]map[string]Operation),
		Components: Components{Schemas: make(map[string]any), SecuritySchemes: make(map[string]SecurityScheme)},
	}
}

// AddOperation registers one explicit operation.
func (document *Document) AddOperation(method, path string, operation Operation) error {
	if document == nil {
		return errors.New("agentapi: OpenAPI document must not be nil")
	}
	method = strings.ToLower(method)
	if !validOpenAPIMethod(method) {
		return fmt.Errorf("agentapi: unsupported OpenAPI method %q", method)
	}
	if !validOpenAPIPath(path) {
		return fmt.Errorf("agentapi: invalid OpenAPI path %q", path)
	}
	if err := validateOperation(operation); err != nil {
		return err
	}
	for _, methods := range document.Paths {
		for _, existing := range methods {
			if existing.ID == operation.ID {
				return fmt.Errorf("agentapi: operation ID %q is already registered", operation.ID)
			}
		}
	}
	if document.Paths == nil {
		document.Paths = make(map[string]map[string]Operation)
	}
	if document.Paths[path] == nil {
		document.Paths[path] = make(map[string]Operation)
	}
	if _, exists := document.Paths[path][method]; exists {
		return fmt.Errorf("agentapi: %s %s is already documented", strings.ToUpper(method), path)
	}
	document.Paths[path][method] = operation
	return nil
}

func (document *Document) validate() error {
	if document.OpenAPI != "3.1.0" || document.Info.Title == "" || document.Info.Version == "" || !safeText(document.Info.Title) || !safeText(document.Info.Version) || !safeText(document.Info.Description) {
		return errors.New("agentapi: invalid OpenAPI document metadata")
	}
	if len(document.Paths) == 0 {
		return errors.New("agentapi: OpenAPI document must contain at least one path")
	}
	ids := make(map[string]bool)
	for path, methods := range document.Paths {
		if !validOpenAPIPath(path) || len(methods) == 0 {
			return fmt.Errorf("agentapi: invalid OpenAPI path %q", path)
		}
		for method, operation := range methods {
			if !validOpenAPIMethod(method) {
				return fmt.Errorf("agentapi: unsupported OpenAPI method %q", method)
			}
			if err := validateOperation(operation); err != nil {
				return fmt.Errorf("agentapi: %s %s: %w", strings.ToUpper(method), path, err)
			}
			if ids[operation.ID] {
				return fmt.Errorf("agentapi: operation ID %q is already registered", operation.ID)
			}
			ids[operation.ID] = true
		}
	}
	return nil
}

func validateOperation(operation Operation) error {
	if operation.ID == "" || operation.Summary == "" || !safeText(operation.ID) || !safeText(operation.Summary) || !safeText(operation.Description) || len(operation.Responses) == 0 {
		return errors.New("operation requires ID, summary, and responses")
	}
	for code, response := range operation.Responses {
		if !validResponseCode(code) || response.Description == "" || !safeText(response.Description) {
			return errors.New("responses require valid codes and descriptions")
		}
	}
	return nil
}

func validOpenAPIPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "\r\n?#")
}

func validResponseCode(code string) bool {
	if code == "default" {
		return true
	}
	if len(code) != 3 || code[0] < '1' || code[0] > '5' {
		return false
	}
	if code[1] == 'X' || code[2] == 'X' {
		return code[1] == 'X' && code[2] == 'X'
	}
	return code[1] >= '0' && code[1] <= '9' && code[2] >= '0' && code[2] <= '9'
}

// AddSchema registers a reusable named schema.
func (document *Document) AddSchema(name string, schema any) error {
	if document == nil || name == "" || strings.ContainsAny(name, "\r\n/#") {
		return errors.New("agentapi: invalid schema name")
	}
	if document.Components.Schemas == nil {
		document.Components.Schemas = make(map[string]any)
	}
	if _, exists := document.Components.Schemas[name]; exists {
		return fmt.Errorf("agentapi: schema %q is already registered", name)
	}
	if _, err := json.Marshal(schema); err != nil {
		return fmt.Errorf("agentapi: schema %q is not JSON serializable: %w", name, err)
	}
	document.Components.Schemas[name] = schema
	return nil
}

func validOpenAPIMethod(method string) bool {
	switch method {
	case "get", "put", "post", "delete", "options", "head", "patch", "trace":
		return true
	default:
		return false
	}
}
