package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	stdhttp "net/http"
	"strings"

	godohttp "github.com/nathanpls/godo/http"
)

// Config configures request IDs.
type Config struct {
	// Header defaults to X-Request-ID.
	Header string

	// Accept optionally trusts and returns an incoming header value. Without
	// Accept, incoming values are ignored and a new ID is generated.
	Accept func(*stdhttp.Request, string) bool

	// Generate optionally creates IDs. The default uses 128 random bits.
	Generate func() (string, error)

	// OnError handles generation failures. The default fails closed with 503.
	OnError func(stdhttp.ResponseWriter, *stdhttp.Request, error)
}

// Plugin installs request ID middleware.
type Plugin struct{ config Config }

// New creates a request ID plugin.
func New(config Config) *Plugin { return &Plugin{config: config} }

// Install validates and installs request ID middleware.
func (plugin *Plugin) Install(router *godohttp.Router) error {
	if plugin == nil || router == nil {
		return errors.New("requestid: plugin and router must not be nil")
	}
	if plugin.config.Header == "" {
		plugin.config.Header = "X-Request-ID"
	}
	if !validHeaderName(plugin.config.Header) {
		return errors.New("requestid: invalid header name")
	}
	if plugin.config.Generate == nil {
		plugin.config.Generate = generate
	}
	if plugin.config.OnError == nil {
		plugin.config.OnError = func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ error) {
			_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusServiceUnavailable, Title: "Request ID unavailable"})
		}
	}
	router.Use(plugin.middleware)
	return nil
}

func (plugin *Plugin) middleware(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		id := ""
		incoming := request.Header.Values(plugin.config.Header)
		if len(incoming) == 1 && incoming[0] != "" && plugin.config.Accept != nil && validID(incoming[0]) && plugin.config.Accept(request, incoming[0]) {
			id = incoming[0]
		}
		if id == "" {
			var err error
			id, err = plugin.config.Generate()
			if err != nil || !validID(id) {
				if err == nil {
					err = errors.New("requestid: generator returned an invalid ID")
				}
				plugin.config.OnError(w, request, err)
				return
			}
		}
		request.Header.Set(plugin.config.Header, id)
		w.Header().Set(plugin.config.Header, id)
		request = request.WithContext(context.WithValue(request.Context(), contextKey{}, id))
		next.ServeHTTP(w, request)
	})
}

type contextKey struct{}

// FromContext returns the request ID attached by the plugin.
func FromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(contextKey{}).(string)
	return id, ok
}

func generate() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "req_" + hex.EncodeToString(value), nil
}

func validID(value string) bool {
	if value == "" || len(value) > 128 {
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
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", character) {
			continue
		}
		return false
	}
	return true
}
