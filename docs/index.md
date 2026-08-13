# godo

`godo` is a dependency-light collection of composable Go packages for building
services. Each package is independent and uses standard library interfaces where
possible.

## Packages

- [CLI](/cli): persistent local Go services and agent discovery
- [HTTP](/http): routing, middleware, JSON responses, and WebSockets
- [HTTP API Keys](/http/plugins/apikey): bearer authentication and key management
- [HTTP Rate Limiting](/http/plugins/ratelimit): memory and shared database limits
- [ORM](/orm): SQLite and PostgreSQL models, migrations, CRUD, and queries

## Agent access

Every documentation URL supports Markdown content negotiation:

```sh
curl http://localhost:8080/http -H "Accept: text/markdown"
```

Browser requests receive HTML. Requests explicitly accepting `text/markdown`
receive the canonical Markdown source directly.
