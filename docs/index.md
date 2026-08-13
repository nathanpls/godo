# godo

`godo` is a dependency-light collection of composable Go packages for building
services. Each package is independent and uses standard library interfaces where
possible.

## Packages

- [HTTP](/http): routing, middleware, JSON responses, and WebSockets

## Agent access

Every documentation URL supports Markdown content negotiation:

```sh
curl http://localhost:8080/http -H "Accept: text/markdown"
```

Browser requests receive HTML. Requests explicitly accepting `text/markdown`
receive the canonical Markdown source directly.
