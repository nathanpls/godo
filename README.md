# godo

`godo` is a collection of small, composable Go packages for building services.
The goal is to own the common building blocks so application development can
focus on the product instead of repeatedly choosing and integrating libraries.

Packages include HTTP services, with database access and rate limiting planned.
Each area lives in a focused top-level package and remains usable independently.

## Packages

- [`godo/http`](./http): method-aware HTTP routing, middleware, JSON responses,
  and WebSockets.

## CLI

Install the `godo` command:

```sh
./cli/install.sh
```

The script builds and installs `godo` to `$HOME/.local/bin/godo`. Alternatively,
use `go install ./cli/godo`.

On Linux, `godo` can build Go programs into persistent `systemd --user`
services and publish their local URLs to OpenCode agents:

```sh
godo service add ./docs \
  --name godo-docs \
  --additions 'Request Accept: text/markdown for canonical documentation'

godo service list
godo service update 1
godo service remove 1
godo agent
```

Managed applications receive their assigned port through the `PORT` environment
variable. See the [CLI documentation](./docs/cli.md) for behavior, storage, and
logging details.

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

Serve the package documentation locally:

```sh
go run ./docs
```

Documentation routes return HTML to browsers and canonical Markdown to agents
that send `Accept: text/markdown`.
