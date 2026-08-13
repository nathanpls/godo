package http

import (
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"reflect"
	"sync"
)

// Middleware wraps an HTTP handler.
type Middleware func(stdhttp.Handler) stdhttp.Handler

// Plugin configures a Router with middleware, routes, or related behavior.
type Plugin interface {
	Install(*Router) error
}

// PluginFunc adapts a function into a Plugin.
type PluginFunc func(*Router) error

// Install configures router by calling function.
func (function PluginFunc) Install(router *Router) error {
	return function(router)
}

// Router dispatches requests by method and path.
//
// Route patterns follow [net/http.ServeMux] syntax. Register routes and
// middleware before serving requests.
type Router struct {
	mux        *stdhttp.ServeMux
	middleware []Middleware
	handler    stdhttp.Handler
	buildOnce  sync.Once
}

// New creates an empty Router.
func New() *Router {
	mux := stdhttp.NewServeMux()
	return &Router{mux: mux}
}

// Use adds middleware in declaration order. The first middleware added is the
// first to receive a request.
func (r *Router) Use(middleware ...Middleware) {
	r.middleware = append(r.middleware, middleware...)
}

// Install adds a plugin to the router. Install plugins before serving requests.
func (r *Router) Install(plugin Plugin) error {
	if plugin == nil || nilValue(plugin) {
		return errors.New("http: plugin must not be nil")
	}
	return plugin.Install(r)
}

func nilValue(value any) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// Handle registers a handler for an HTTP method and path pattern. It accepts
// extension methods in addition to the methods with named helpers.
func (r *Router) Handle(method, pattern string, handler stdhttp.Handler) {
	r.mux.Handle(method+" "+pattern, handler)
}

// HandleFunc registers a handler function for an HTTP method and path pattern.
func (r *Router) HandleFunc(method, pattern string, handler stdhttp.HandlerFunc) {
	r.Handle(method, pattern, handler)
}

// Connect registers a CONNECT handler.
func (r *Router) Connect(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodConnect, pattern, handler)
}

// Delete registers a DELETE handler.
func (r *Router) Delete(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodDelete, pattern, handler)
}

// Get registers a GET handler. Go's HTTP server also sends HEAD requests to a
// matching GET route unless a more specific HEAD route exists.
func (r *Router) Get(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodGet, pattern, handler)
}

// Head registers a HEAD handler.
func (r *Router) Head(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodHead, pattern, handler)
}

// Options registers an OPTIONS handler.
func (r *Router) Options(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodOptions, pattern, handler)
}

// Patch registers a PATCH handler.
func (r *Router) Patch(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodPatch, pattern, handler)
}

// Post registers a POST handler.
func (r *Router) Post(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodPost, pattern, handler)
}

// Put registers a PUT handler.
func (r *Router) Put(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodPut, pattern, handler)
}

// Query registers a QUERY handler as defined by RFC 9842.
func (r *Router) Query(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc("QUERY", pattern, handler)
}

// Trace registers a TRACE handler.
func (r *Router) Trace(pattern string, handler stdhttp.HandlerFunc) {
	r.HandleFunc(stdhttp.MethodTrace, pattern, handler)
}

// ServeHTTP dispatches a request to the matching route.
func (r *Router) ServeHTTP(w stdhttp.ResponseWriter, request *stdhttp.Request) {
	r.buildOnce.Do(func() {
		var handler stdhttp.Handler = r.mux
		for i := len(r.middleware) - 1; i >= 0; i-- {
			handler = r.middleware[i](handler)
		}
		r.handler = handler
	})
	r.handler.ServeHTTP(w, request)
}

// JSON writes value as a JSON response with the given status code.
func JSON(w stdhttp.ResponseWriter, status int, value any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(value)
}
