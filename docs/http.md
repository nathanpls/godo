# HTTP

Package `github.com/nathanpls/godo/core/http` provides method-aware routing, middleware, JSON responses,
and server-side WebSockets using only the Go standard library.

```go
import (
    stdhttp "net/http"

    godohttp "github.com/nathanpls/godo/core/http"
)
```

## Router

Create a router, register routes, and pass it anywhere an `http.Handler` is
accepted:

```go
router := godohttp.New()

router.Get("/users/{id}", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
    id := r.PathValue("id")
    _ = godohttp.JSON(w, stdhttp.StatusOK, map[string]string{"id": id})
})

stdhttp.ListenAndServe(":8080", router)
```

Route patterns follow `net/http.ServeMux` syntax. Path parameters are available
through `r.PathValue(name)`.

### Methods

The router has helpers for every standard method:

```go
router.Connect(pattern, handler)
router.Delete(pattern, handler)
router.Get(pattern, handler)
router.Head(pattern, handler)
router.Options(pattern, handler)
router.Patch(pattern, handler)
router.Post(pattern, handler)
router.Put(pattern, handler)
router.Query(pattern, handler)
router.Trace(pattern, handler)
```

`Query` registers the `QUERY` method defined by RFC 9842. Extension methods can
be registered directly:

```go
router.HandleFunc("CUSTOM", "/resource", handler)
```

### Middleware

Middleware uses standard `http.Handler` values. It runs in declaration order:

```go
func logRequests(next stdhttp.Handler) stdhttp.Handler {
    return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
        log.Printf("%s %s", r.Method, r.URL.Path)
        next.ServeHTTP(w, r)
    })
}

router.Use(logRequests)
```

Register routes and middleware before serving requests.

### Plugins

Plugins configure middleware, routes, or related behavior through one validated
installation step:

```go
if err := router.Install(plugin); err != nil {
    log.Fatal(err)
}
```

Implement a plugin with `Install(*http.Router) error`, or adapt a small function:

```go
plugin := godohttp.PluginFunc(func(router *godohttp.Router) error {
    router.Use(middleware)
    router.Get("/health", healthHandler)
    return nil
})
```

Use `Router.Use` for ordinary middleware and `Router.Install` for reusable
packages that need configuration, validation, or route registration. Install
plugins before the router serves its first request.

Available plugins:

- [API key authentication](/http/plugins/apikey): hashed bearer keys managed by
  the godo CLI
- [Agent API discovery](/http/plugins/agentapi): discovery manifest, explicit
  OpenAPI 3.1 contract, and `llms.txt`
- [Idempotency](/http/plugins/idempotency): bounded response replay for retry-safe
  mutations
- [Request IDs](/http/plugins/requestid): generated or explicitly trusted
  correlation IDs
- [Rate limiting](/http/plugins/ratelimit): memory or shared SQLite/PostgreSQL
  fixed-window limits

### JSON

`JSON` sets the response content type, writes the status, and encodes a value:

```go
if err := godohttp.JSON(w, stdhttp.StatusCreated, value); err != nil {
    log.Printf("write response: %v", err)
}
```

`DecodeJSON` strictly decodes one bounded request value. It requires an
`application/json` or `+json` media type and rejects unknown fields, malformed
bodies, trailing values, and oversized input:

```go
if err := godohttp.DecodeJSON(r, &input, 1<<20); err != nil {
    var decode *godohttp.DecodeError
    if errors.As(err, &decode) {
        _ = godohttp.WriteProblem(w, decode.Problem())
        return
    }
    _ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusInternalServerError})
    return
}
```

`DecodeError` exposes only a client-safe status and detail. Its wrapped decoder
error is available for server-side logging but is not included in `Problem`.

### Problem responses

`WriteProblem` writes RFC 9457 `application/problem+json` responses. The title
defaults to the HTTP status text and the type defaults to `about:blank`:

```go
_ = godohttp.WriteProblem(w, godohttp.Problem{
    Status:    stdhttp.StatusBadRequest,
    Detail:    "limit must be between 1 and 100",
    RequestID: requestID,
})
```

Extensions become additional top-level members. Reserved problem members cannot
be replaced through `Extensions`.

### Cursor pagination

`ParsePagination` validates singular `limit` and `cursor` query parameters:

```go
pagination, err := godohttp.ParsePagination(r, 25, 100)
if err != nil {
    _ = godohttp.WriteProblem(w, godohttp.Problem{Status: stdhttp.StatusBadRequest, Detail: err.Error()})
    return
}
```

Return collections with `Page[T]`. `EncodeCursor` and `DecodeCursor` provide
opaque URL-safe JSON cursors; they are not encrypted or signed and must not
contain secrets.

## WebSockets

`WebSocket` registers a GET route, performs the RFC 6455 handshake, and closes
the connection when the handler returns:

```go
router.WebSocket("/events", func(conn *godohttp.Conn, r *stdhttp.Request) {
    for {
        messageType, message, err := conn.ReadMessage()
        if err != nil {
            return
        }
        if err := conn.WriteMessage(messageType, message); err != nil {
            return
        }
    }
})
```

The connection supports text and binary messages, fragmented messages,
automatic ping/pong handling, close frames, and concurrent reading and writing.
Multiple writers are serialized.

JSON messages have dedicated helpers:

```go
var event Event
if err := conn.ReadJSON(&event); err != nil {
    return
}
if err := conn.WriteJSON(event); err != nil {
    return
}
```

By default, messages are limited to 1 MiB and browser origins must match the
request host. Use `Upgrader` when custom behavior is needed:

```go
upgrader := godohttp.Upgrader{
    MaxMessageSize: 4 << 20,
    CheckOrigin: func(r *stdhttp.Request) bool {
        return r.Header.Get("Origin") == "https://app.example.com"
    },
}

conn, err := upgrader.Upgrade(w, r)
if err != nil {
    return
}
defer conn.Close()
```

Use `SetReadDeadline` and `SetWriteDeadline` when connections need application
specific timeout behavior.

`DialWebSocket` opens an RFC 6455 client connection for `ws` or `wss`, validates
the server handshake, and applies client-role frame masking. The context controls
dialing and the opening handshake:

```go
conn, err := godohttp.DialWebSocket(ctx, "wss://example.com/events", nil)
if err != nil {
    return err
}
defer conn.Close()
```

Use `Abort` when a protocol requires dropping a resumable connection without a
normal close frame.
