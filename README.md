# godo

`godo` is a collection of small, composable Go packages for building services.
The goal is to own the common building blocks so application development can
focus on the product instead of repeatedly choosing and integrating libraries.

Packages include HTTP services and database access, with rate limiting planned.
Each area lives in a focused top-level package and remains usable independently.

## Packages

- [`godo/http`](./http): method-aware HTTP routing, middleware, JSON responses,
  and WebSockets.
- [`godo/http/plugins/ratelimit`](./http/plugins/ratelimit): fixed-window request
  limits using memory, SQLite, or PostgreSQL.
- [`godo/http/plugins/apikey`](./http/plugins/apikey): hashed bearer API keys
  managed through the `godo` CLI.
- [`godo/orm`](./orm): driver-neutral SQLite and PostgreSQL models, migrations,
  CRUD, queries, and transactions.

## CLI

Install the `godo` command:

```sh
./cli/install.sh
```

The script builds and installs `godo` to `$HOME/.local/bin/godo`. Alternatively,
use `go install ./cli/godo`.

Start a minimal project and add only the dependencies it needs:

```sh
godo init my-api --module github.com/example/my-api
cd my-api
godo add http
```

`godo init` creates `go.mod`, `main.go`, and `.gitignore` in an empty directory.
`godo add` runs `go get` for a known godo package or SQL driver without editing
application source. Use `godo <command> --help` for command-specific guidance.

Resolve or search the exact godo source selected by the current codebase:

```sh
godo source http/plugins/apikey
godo source search "func (plugin *Plugin) middleware" \
  --package http/plugins/apikey --context 5
```

The command follows the project's selected godo version, workspace, and local
replacements instead of relying on a fixed module-cache path.

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

The CLI also compiles registered ORM models into reviewable SQLite or PostgreSQL
migrations:

```sh
godo db init --dialect sqlite
godo db generate create_users
godo db status
godo db migrate
godo db rollback
```

See the [ORM documentation](./docs/orm.md) for model registration and migration
safety rules.

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
