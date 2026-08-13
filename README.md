# godo

`godo` is a collection of small, composable Go packages for building services.
The goal is to own the common building blocks so application development can
focus on the product instead of repeatedly choosing and integrating libraries.

Planned areas include database access, HTTP services, and rate limiting. Each
area will live in a focused top-level package and remain usable independently.

## Principles

- Prefer the standard library.
- Add dependencies only when implementing the capability ourselves is unsafe or
  disproportionately complex.
- Keep APIs small and explicit.
- Make packages composable rather than coupling them through a framework core.
- Add packages when they are needed, not in anticipation of future use.

## Layout

The repository is one Go module containing independent packages:

```text
godo/
├── go.mod
├── doc.go
└── <package>/
```

This single-module layout keeps local development, versioning, and releases
simple. A package can be split into its own module later if independent
versioning becomes necessary.

## Development

Format and verify all packages before committing:

```sh
go fmt ./...
go vet ./...
go test ./...
```
