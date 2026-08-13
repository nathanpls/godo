# HTTP Request IDs

Package `github.com/nathanpls/godo/http/plugins/requestid` attaches a correlation
ID to each request and response.

## Install

```sh
godo add requestid
```

```go
if err := router.Install(requestid.New(requestid.Config{})); err != nil {
    log.Fatal(err)
}
```

The default generates 128 random bits and returns an ID such as
`req_43e5...` in `X-Request-ID`. Incoming IDs are ignored unless an explicit
`Accept` function trusts them:

```go
requestid.New(requestid.Config{
    Accept: func(r *http.Request, id string) bool {
        return r.Header.Get("X-Trusted-Proxy") == "true"
    },
})
```

Only trust IDs from a verified proxy boundary. Values must contain visible ASCII
and are limited to 128 bytes. Duplicate incoming headers are never trusted.

Read the ID in a handler with:

```go
id, ok := requestid.FromContext(r.Context())
```

Install request ID middleware before authentication, authorization, rate
limiting, and idempotency so every response can carry the current request ID.
