# HTTP Rate Limiting

Package `github.com/nathanpls/godo/http/plugins/ratelimit` installs fixed-window
request rate limiting on a `godo/http` router.

## In-memory limits

Install a process-local limiter without database setup:

```go
import (
    "log"
    "time"

    godohttp "github.com/nathanpls/godo/http"
    "github.com/nathanpls/godo/http/plugins/ratelimit"
)

router := godohttp.New()
if err := router.Install(ratelimit.New(ratelimit.Config{
    Limit:  100,
    Window: time.Minute,
})); err != nil {
    log.Fatal(err)
}
```

The default `MemoryStore` is concurrency-safe, keeps at most 100,000 active
keys, and removes expired keys in expiration order. It is appropriate for one
application process. Separate processes enforce separate limits.

## Shared ORM limits

Use `ORMStore` to coordinate limits through SQLite or PostgreSQL:

```go
store := ratelimit.NewORMStore(db)

if err := router.Install(ratelimit.New(ratelimit.Config{
    Store:     store,
    Limit:     100,
    Window:    time.Minute,
    Namespace: "public-api",
})); err != nil {
    log.Fatal(err)
}
```

Register the rate-limit bucket in `db/godo/main.go` so the table is created by a
reviewable migration:

```go
Models: []any{
    models.User{},
    ratelimit.Bucket{},
},
```

Then generate and apply it:

```sh
godo db generate add_rate_limits
godo db migrate
```

For local development without migration files:

```go
if err := store.Migrate(ctx); err != nil {
    log.Fatal(err)
}
```

The ORM store uses an atomic upsert and the database clock. It periodically
deletes expired buckets. SQLite and PostgreSQL can therefore coordinate limits
across processes that share the same database.

## Client keys

The default key is the direct remote IP from `Request.RemoteAddr`. It
deliberately ignores `X-Forwarded-For` and similar headers because clients can
forge them unless the server validates a trusted proxy.

Behind a trusted proxy, supply an explicit key function that accepts forwarded
headers only when `RemoteAddr` belongs to that proxy:

```go
Key: func(r *http.Request) string {
    if isTrustedProxy(r.RemoteAddr) {
        return validatedForwardedIP(r.Header.Get("X-Forwarded-For"))
    }
    return ratelimit.IPKey(r)
},
```

Keys are limited to 1,024 bytes. Use `Namespace` to isolate unrelated policies
stored in the same backend.

## Bypass selected requests

```go
Skip: func(r *http.Request) bool {
    return r.URL.Path == "/health"
},
```

## Responses

Allowed and denied responses include these legacy fields:

| Header | Meaning |
|---|---|
| `RateLimit-Limit` | Requests allowed in the current fixed window |
| `RateLimit-Remaining` | Requests remaining in the current window |
| `RateLimit-Reset` | Relative seconds until the next window |
| `Retry-After` | Relative seconds until retry; denied responses only |

The default denied response is HTTP `429 Too Many Requests`. Replace it when an
API needs JSON or another response format:

```go
Denied: func(w http.ResponseWriter, r *http.Request, result ratelimit.Result) {
    _ = godohttp.JSON(w, http.StatusTooManyRequests, map[string]any{
        "error": "rate limit exceeded",
        "reset": result.Reset,
    })
},
```

Storage errors fail closed with HTTP `503 Service Unavailable` and do not expose
the internal error. Supply `OnError` to log the error or intentionally choose a
different policy:

```go
OnError: func(w http.ResponseWriter, r *http.Request, err error) {
    log.Printf("rate limit store: %v", err)
    http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
},
```

## Store interface

Implement another atomic backend with the small `Store` interface:

```go
type Store interface {
    Take(
        context.Context,
        string,
        int64,
        time.Duration,
        time.Time,
    ) (ratelimit.Result, error)
}
```

`Take` must consume one allowed request atomically. A backend shared by multiple
processes should use a shared clock and atomic compare/update operation.
