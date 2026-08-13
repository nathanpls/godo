# HTTP API Key Authentication

Package `github.com/nathanpls/godo/http/plugins/apikey` installs bearer API key
authentication on a `godo/http` router.

## Add the package

```sh
godo add apikey
```

This adds the package dependency without editing application source.

## Initialize key storage

From the application module:

```sh
godo auth init
```

This creates:

```text
.godo/
├── .gitignore
└── auth.json
```

The directory is mode `0700`, `auth.json` is mode `0600`, and `.gitignore`
excludes all secrets and lock files under `.godo`.

Keys are stored as SHA-256 hashes. Plaintext secrets are never written to the
file.

## Create a key

```sh
godo auth create --name opencode
```

Example output:

```text
Created API key 1: opencode
Secret (shown once):
godo_7f3a19c2_...
```

The token contains 256 random bits and is displayed once. Store it in the
agent's secret configuration immediately; it cannot be recovered from
`auth.json`.

## Install the plugin

```go
import (
    "log"

    godohttp "github.com/nathanpls/godo/http"
    "github.com/nathanpls/godo/http/plugins/apikey"
)

router := godohttp.New()

auth := apikey.New(apikey.Config{
    Store: apikey.NewFileStore(".godo/auth.json"),
    Realm: "slopdown",
})

if err := router.Install(auth); err != nil {
    log.Fatal(err)
}
```

Install the plugin before serving the router. Protected requests authenticate
with:

```http
Authorization: Bearer godo_7f3a19c2_...
```

Missing, malformed, duplicate, or invalid authorization fields receive HTTP
`401 Unauthorized` with a bearer `WWW-Authenticate` challenge.

## Authenticated identity

The plugin attaches non-secret key metadata to the request context:

```go
router.Post("/api/plans", func(w http.ResponseWriter, r *http.Request) {
    key, ok := apikey.KeyFromContext(r.Context())
    if !ok {
        http.Error(w, "missing identity", http.StatusInternalServerError)
        return
    }

    log.Printf("request from API key %d: %s", key.ID, key.Name)
})
```

`Key` includes the numeric ID, name, non-secret prefix, and creation time. It
never includes the token or hash.

## Public routes

Bypass authentication explicitly for health checks or public pages:

```go
Skip: func(r *http.Request) bool {
    return r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/plans/")
},
```

The bypass applies before parsing credentials.

## Custom responses

The default unauthorized response is plain-text HTTP `401`. APIs can provide a
JSON response:

```go
Unauthorized: func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("WWW-Authenticate", `Bearer realm="slopdown"`)
    _ = godohttp.JSON(w, http.StatusUnauthorized, map[string]string{
        "error": "unauthorized",
    })
},
```

Storage errors fail closed with HTTP `503 Service Unavailable` and do not expose
internal details. Use `OnError` to log failures:

```go
OnError: func(w http.ResponseWriter, r *http.Request, err error) {
    log.Printf("API key authentication: %v", err)
    http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
},
```

## Manage keys

List non-secret key metadata:

```sh
godo auth list
```

Revoke a key by ID:

```sh
godo auth revoke 1
```

The file store reads `auth.json` during each authentication. Created and revoked
keys therefore take effect immediately without rebuilding or restarting the
service.

IDs increase monotonically and are not reused after revocation.

## Custom stores

Implement another backend with:

```go
type Store interface {
    Authenticate(token string) (apikey.Key, bool, error)
}
```

Stores should compare credential material in constant time and return only
non-secret identity metadata.
