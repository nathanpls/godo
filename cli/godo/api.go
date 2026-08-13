package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	stdhttp "net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nathanpls/godo/http/plugins/agentapi"
)

const maxAPICheckBody = 4 << 20

func (a *app) runAPI(arguments []string) error {
	if len(arguments) == 0 || isHelp(arguments) {
		return printHelp(a.stdout, apiHelp)
	}
	if arguments[0] != "check" {
		return fmt.Errorf("unknown api command %q; run godo api --help", arguments[0])
	}
	if isHelp(arguments[1:]) {
		return printHelp(a.stdout, apiCheckHelp)
	}
	if len(arguments) != 2 {
		return errors.New("api check requires one base URL")
	}
	return a.checkAPI(arguments[1])
}

func (a *app) checkAPI(base string) error {
	baseURL, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return fmt.Errorf("invalid API base URL %q", base)
	}
	client := &stdhttp.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(request *stdhttp.Request, via []*stdhttp.Request) error {
			if !sameOrigin(baseURL, request.URL) {
				return errors.New("redirect left the checked API origin")
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	manifestURL := baseURL.String() + "/.well-known/godo.json"
	manifestBody, manifestHeader, err := fetchCheck(ctx, client, manifestURL, "application/json")
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	var manifest agentapi.Manifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return fmt.Errorf("discovery: decode manifest: %w", err)
	}
	if manifest.Name == "" || manifest.Version == "" || manifest.OpenAPI == "" || manifest.LLMs == "" {
		return errors.New("discovery: manifest requires name, version, openapi, and llms")
	}
	if !safeCheckText(manifest.Name) || !safeCheckText(manifest.Version) || len(manifest.Name) > 200 || len(manifest.Version) > 100 {
		return errors.New("discovery: name or version contains invalid characters")
	}
	fmt.Fprintf(a.stdout, "PASS discovery: %q %q\n", manifest.Name, manifest.Version)
	if requestID := manifestHeader.Get("X-Request-ID"); requestID != "" {
		if !safeCheckText(requestID) || len(requestID) > 128 {
			return errors.New("request IDs: invalid X-Request-ID")
		}
		fmt.Fprintf(a.stdout, "PASS request IDs: %q\n", requestID)
	} else {
		fmt.Fprintln(a.stdout, "WARN request IDs: X-Request-ID was not returned")
	}

	openAPIURL, err := resolveAPIURL(baseURL, manifest.OpenAPI)
	if err != nil {
		return fmt.Errorf("OpenAPI: %w", err)
	}
	openAPIBody, _, err := fetchCheck(ctx, client, openAPIURL, "application/json")
	if err != nil {
		return fmt.Errorf("OpenAPI: %w", err)
	}
	var document struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(openAPIBody, &document); err != nil || document.OpenAPI != "3.1.0" || document.Info.Title == "" || document.Info.Version == "" || len(document.Paths) == 0 {
		return errors.New("OpenAPI: expected a valid OpenAPI 3.1 document with paths")
	}
	fmt.Fprintf(a.stdout, "PASS OpenAPI: %d documented paths\n", len(document.Paths))

	llmsURL, err := resolveAPIURL(baseURL, manifest.LLMs)
	if err != nil {
		return fmt.Errorf("llms.txt: %w", err)
	}
	llmsBody, llmsHeader, err := fetchCheck(ctx, client, llmsURL, "text/plain")
	if err != nil || len(strings.TrimSpace(string(llmsBody))) == 0 || !strings.HasPrefix(llmsHeader.Get("Content-Type"), "text/plain") {
		return errors.New("llms.txt: expected non-empty text/plain content")
	}
	fmt.Fprintln(a.stdout, "PASS llms.txt")

	if manifest.Documentation != "" {
		docsURL, err := resolveAPIURL(baseURL, manifest.Documentation)
		if err != nil {
			return fmt.Errorf("documentation: %w", err)
		}
		docsBody, docsHeader, err := fetchCheck(ctx, client, docsURL, "text/markdown")
		if err != nil || len(strings.TrimSpace(string(docsBody))) == 0 || !strings.HasPrefix(docsHeader.Get("Content-Type"), "text/markdown") {
			return errors.New("documentation: expected non-empty text/markdown for Accept: text/markdown")
		}
		fmt.Fprintln(a.stdout, "PASS Markdown documentation")
	} else {
		fmt.Fprintln(a.stdout, "WARN Markdown documentation: no documentation URL declared")
	}

	if manifest.Authentication.Type == "bearer" {
		if !strings.EqualFold(manifest.Authentication.Header, "Authorization") || !strings.EqualFold(manifest.Authentication.Scheme, "Bearer") {
			return errors.New("authentication: bearer APIs must declare Authorization and Bearer")
		}
		fmt.Fprintln(a.stdout, "PASS bearer authentication metadata")
	} else if manifest.Authentication.Type == "" {
		fmt.Fprintln(a.stdout, "WARN authentication: no authentication declared")
	} else {
		return fmt.Errorf("authentication: unsupported type %q", manifest.Authentication.Type)
	}
	return nil
}

func fetchCheck(ctx context.Context, client *stdhttp.Client, target, accept string) ([]byte, stdhttp.Header, error) {
	request, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, target, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Accept", accept)
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != stdhttp.StatusOK {
		return nil, nil, fmt.Errorf("GET %s returned %s", target, response.Status)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !acceptedMediaType(accept, mediaType) {
		return nil, nil, fmt.Errorf("GET %s returned Content-Type %q, expected %s", target, response.Header.Get("Content-Type"), accept)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAPICheckBody+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxAPICheckBody {
		return nil, nil, errors.New("response exceeds 4 MiB")
	}
	return body, response.Header.Clone(), nil
}

func resolveAPIURL(base *url.URL, target string) (string, error) {
	reference, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	resolved := base.ResolveReference(reference)
	if !sameOrigin(base, resolved) || resolved.User != nil {
		return "", errors.New("endpoint must remain on the checked API origin")
	}
	return resolved.String(), nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func acceptedMediaType(accept, actual string) bool {
	switch accept {
	case "application/json":
		return actual == "application/json" || strings.HasSuffix(actual, "+json")
	default:
		return actual == accept
	}
}

func safeCheckText(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
