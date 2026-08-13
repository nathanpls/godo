# HTTP Agent API

Package `github.com/nathanpls/godo/core/http/plugins/agentapi` publishes a small,
agent-readable discovery contract:

- `/.well-known/godo.json`: service metadata and endpoint locations
- `/openapi.json`: an explicit OpenAPI 3.1 document
- `/llms.txt`: concise links for language models

## Install

```sh
godo add agentapi
```

## Define the contract

Routes are documented explicitly rather than inferred from handlers:

```go
document := agentapi.NewOpenAPI("Plans API", "1.0.0")

err := document.AddOperation("POST", "/plans", agentapi.Operation{
    ID:      "createPlan",
    Summary: "Create a plan",
    Responses: map[string]agentapi.Response{
        "201": {Description: "Created"},
        "400": {Description: "Invalid request"},
    },
})
if err != nil {
    log.Fatal(err)
}

if err := router.Install(agentapi.New(agentapi.Config{
    Name:          "plans",
    Version:       "1.0.0",
    Description:   "Create and share plans",
    Documentation: "/docs",
    Authentication: agentapi.Authentication{
        Type:   "bearer",
        Header: "Authorization",
        Scheme: "Bearer",
    },
    OpenAPI: document,
})); err != nil {
    log.Fatal(err)
}
```

Use `AddSchema` for reusable component schemas and `$ref` values in operations.
`AddOperation` rejects unsupported methods, duplicate operations, duplicate
operation IDs, and incomplete operations. Installation validates and serializes
the complete document before registering routes, so later document mutation does
not change the served contract.

If authentication middleware is installed before this plugin, configure its
`Skip` function for all three discovery paths when the contract should be public.

## Check a service

```sh
godo api check https://api.example.com
```

The read-only check validates discovery, OpenAPI 3.1, `llms.txt`, optional
Markdown documentation, bearer metadata, and request ID response headers.
Manifest links and redirects must remain on the exact supplied origin.
