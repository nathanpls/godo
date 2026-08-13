# HTTP Idempotency

Package `github.com/nathanpls/godo/core/http/plugins/idempotency` coordinates mutation
requests by `Idempotency-Key` and replays a completed response for retries.

## Install

```sh
godo add idempotency
```

```go
if err := router.Install(idempotency.New(idempotency.Config{
    Require: true,
    TTL:     24 * time.Hour,
})); err != nil {
    log.Fatal(err)
}
```

By default the plugin handles `POST`, `PUT`, `PATCH`, and `DELETE`. Equal keys
from the same bearer principal coordinate one execution. A retry with the same
method, URI, content type, and body receives the stored status, body, and safe
representation headers plus:

```http
Idempotency-Replayed: true
```

Reusing the key for a different request returns `409 Conflict`. A completed
`5xx` response is replayed rather than executing the mutation again. If a
handler panics or its response exceeds `MaxResponseBytes`, the key becomes a
terminal `422 Unprocessable Entity` record because the mutation outcome may be
ambiguous.

## Middleware order

Install authentication, scope enforcement, and rate limiting before idempotency:

```go
router.Install(requestid.New(requestid.Config{}))
router.Install(auth)
router.Use(requireWriteScope)
router.Install(limiter)
router.Install(idempotency.New(idempotency.Config{Require: true}))
```

Middleware runs in declaration order. Keeping authorization outside replay means
revoked keys, changed scopes, and current rate limits are still checked on every
retry.

The default scope hashes the `Authorization` header. Applications using cookies
or another authentication mechanism must set `Config.Scope` to a stable,
non-secret principal identifier. Do not use user-controlled identity values.

## Limits

The implementation buffers request and response bodies. It is intended for
bounded JSON-style mutation handlers, not streaming, WebSocket, server-sent
event, or response-controller handlers.

The store is in memory and process-local. Restarts lose records. Multiple
instances require sticky routing for equal principal/key pairs; otherwise the
same mutation can execute on different instances. Configure `MaxEntries`,
`MaxTotalBytes`, `MaxRequestBytes`, `MaxResponseBytes`, and `MaxWaiters` for the
service's traffic and memory budget.
