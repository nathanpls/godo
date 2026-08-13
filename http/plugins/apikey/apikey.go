package apikey

import (
	"context"
	"errors"
	stdhttp "net/http"
	"reflect"
	"slices"
	"strings"

	godohttp "github.com/nathanpls/godo/http"
)

// Store verifies bearer tokens.
type Store interface {
	Authenticate(string) (Key, bool, error)
}

// UnauthorizedHandler handles missing or invalid credentials.
type UnauthorizedHandler func(stdhttp.ResponseWriter, *stdhttp.Request)

// ErrorHandler handles authentication store failures.
type ErrorHandler func(stdhttp.ResponseWriter, *stdhttp.Request, error)

// Config configures API key authentication.
type Config struct {
	Store Store

	// Realm is included in the WWW-Authenticate challenge. It defaults to
	// "api".
	Realm string

	// Skip bypasses authentication when it returns true.
	Skip func(*stdhttp.Request) bool

	// Unauthorized handles missing or invalid keys. The default writes 401.
	Unauthorized UnauthorizedHandler

	// OnError handles store failures. The default fails closed with 503.
	OnError ErrorHandler
}

// Plugin installs bearer API key authentication middleware.
type Plugin struct {
	config Config
}

// New creates an API key authentication plugin.
func New(config Config) *Plugin {
	return &Plugin{config: config}
}

// Install validates and installs the plugin.
func (plugin *Plugin) Install(router *godohttp.Router) error {
	if plugin == nil {
		return errors.New("apikey: plugin must not be nil")
	}
	if router == nil {
		return errors.New("apikey: router must not be nil")
	}
	if plugin.config.Store == nil || nilInterface(plugin.config.Store) {
		return errors.New("apikey: store must not be nil")
	}
	if !validRealm(plugin.config.Realm) {
		return errors.New("apikey: realm contains invalid characters")
	}
	if plugin.config.Realm == "" {
		plugin.config.Realm = "api"
	}
	if plugin.config.Unauthorized == nil {
		realm := plugin.config.Realm
		plugin.config.Unauthorized = func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+realm+`"`)
			_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusUnauthorized, Title: "Unauthorized"})
		}
	}
	if plugin.config.OnError == nil {
		plugin.config.OnError = func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ error) {
			_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusServiceUnavailable, Title: "Authentication unavailable"})
		}
	}
	router.Use(plugin.middleware)
	return nil
}

func (plugin *Plugin) middleware(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
		if plugin.config.Skip != nil && plugin.config.Skip(request) {
			next.ServeHTTP(w, request)
			return
		}
		authorization := request.Header.Values("Authorization")
		if len(authorization) != 1 {
			plugin.config.Unauthorized(w, request)
			return
		}
		token, ok := bearerToken(authorization[0])
		if !ok {
			plugin.config.Unauthorized(w, request)
			return
		}
		identity, valid, err := plugin.config.Store.Authenticate(token)
		if err != nil {
			plugin.config.OnError(w, request, err)
			return
		}
		if !valid {
			plugin.config.Unauthorized(w, request)
			return
		}
		request = request.WithContext(context.WithValue(request.Context(), identityContextKey{}, identity))
		next.ServeHTTP(w, request)
	})
}

func validRealm(realm string) bool {
	for _, character := range realm {
		if character == '\\' || character == '"' || character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

type identityContextKey struct{}

// KeyFromContext returns the authenticated key identity attached to a request.
func KeyFromContext(ctx context.Context) (Key, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Key)
	return identity, ok
}

// HasScope reports whether key contains scope.
func (key Key) HasScope(scope string) bool {
	return slices.Contains(key.Scopes, scope)
}

// Require returns middleware that permits only authenticated keys containing
// every requested scope. Install API key authentication before this middleware.
func Require(scopes ...string) (godohttp.Middleware, error) {
	normalized, err := normalizeScopes(scopes)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return nil, errors.New("apikey: Require needs at least one scope")
	}
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, request *stdhttp.Request) {
			key, ok := KeyFromContext(request.Context())
			if !ok {
				_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusUnauthorized, Title: "Authentication required"})
				return
			}
			for _, scope := range normalized {
				if !key.HasScope(scope) {
					_ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusForbidden, Title: "Insufficient scope", Extensions: map[string]any{"required_scopes": normalized}})
					return
				}
			}
			next.ServeHTTP(w, request)
		})
	}, nil
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func nilInterface(value any) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
