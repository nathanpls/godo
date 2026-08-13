# HTTP

Package `github.com/nathanpls/godo/http` provides method-aware routing, middleware, JSON responses,
and server-side WebSockets using only the Go standard library.

```go
import (
    stdhttp "net/http"

    godohttp "github.com/nathanpls/godo/http"
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

### JSON

`JSON` sets the response content type, writes the status, and encodes a value:

```go
if err := godohttp.JSON(w, stdhttp.StatusCreated, value); err != nil {
    log.Printf("write response: %v", err)
}
```

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
