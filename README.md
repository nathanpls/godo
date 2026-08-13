# godo

`godo` is a collection of small, composable Go packages for building services.
The goal is to own the common building blocks so application development can
focus on the product instead of repeatedly choosing and integrating libraries.

Packages include HTTP services, agent-facing API primitives, database access,
and communication channels. Each package remains usable independently under a
focused `core` or `channels` group.

## Packages

- [`godo/core/http`](./core/http): method-aware HTTP routing, middleware, JSON responses,
  and WebSockets.
- [`godo/channels/discord`](./channels/discord): Discord bot REST operations, Gateway events,
  threads, reactions, and slash commands.
- [`godo/core/http/plugins/ratelimit`](./core/http/plugins/ratelimit): fixed-window request
  limits using memory, SQLite, or PostgreSQL.
- [`godo/core/http/plugins/apikey`](./core/http/plugins/apikey): hashed bearer API keys
  with scopes, managed through the `godo` CLI.
- [`godo/core/http/plugins/agentapi`](./core/http/plugins/agentapi): discovery manifests,
  explicit OpenAPI 3.1 contracts, and `llms.txt`.
- [`godo/core/http/plugins/idempotency`](./core/http/plugins/idempotency): bounded,
  process-local mutation response replay.
- [`godo/core/http/plugins/requestid`](./core/http/plugins/requestid): generated or trusted
  request correlation IDs.
- [`godo/core/orm`](./core/orm): driver-neutral SQLite and PostgreSQL models, migrations,
  CRUD, queries, and transactions.
- [`godo/core/id`](./core/id): opaque cryptographically random resource identifiers.
- [`godo/core/lifecycle`](./core/lifecycle): coordinated services and bounded graceful
  shutdown.
- [`godo/core/password`](./core/password): Argon2id password hashing, verification, and
  rehash detection.
- [`godo/core/validate`](./core/validate): explicit validation for request and domain data.

## Roadmap

Production-grade API building blocks, preferring godo and the standard library
over additional dependencies:

- [x] HTTP routing and middleware (`godo/core/http`, `net/http`)
- [x] JSON and RFC 9457 problem responses
- [x] PostgreSQL and SQLite database access (`godo/core/orm` plus a SQL driver)
- [x] Models, CRUD, queries, and transactions (`godo/core/orm`)
- [x] Database migration generation and execution (`godo db`)
- [x] API key authentication and scopes
- [x] Request IDs, rate limiting, and idempotency
- [x] Cursor pagination
- [x] Explicit OpenAPI 3.1 and agent discovery
- [x] Structured logging (`log/slog`)
- [x] Environment-based configuration (`os.Getenv`)
- [x] Testing (`testing` and `httptest`)
- [x] Request payload validation
- [ ] JWT authentication
- [x] Password hashing helpers (Argon2id)
- [ ] Tracing and metrics with OpenTelemetry
- [x] Service lifecycle helpers for graceful shutdown and background goroutines
- [x] ID generation helpers when database-generated IDs are unsuitable

Unchecked items are candidates, not dependency commitments. They should be
added only when a real service needs them and the standard library is not
enough.

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

Generate a Discord bot starter when that application shape is wanted:

```sh
godo init my-bot --module github.com/example/my-bot --template discord
cd my-bot
godo add discord
```

`godo init` creates `go.mod`, `main.go`, and `.gitignore` in an empty directory.
`godo add` runs `go get` for a known godo package or SQL driver without editing
application source. Use `godo <command> --help` for command-specific guidance.

Resolve or search the exact godo source selected by the current codebase:

```sh
godo source core/http/plugins/apikey
godo source search "func (plugin *Plugin) middleware" \
  --package core/http/plugins/apikey --context 5
```

The command follows the project's selected godo version, workspace, and local
replacements instead of relying on a fixed module-cache path.

Validate an API's agent-readable discovery contract:

```sh
godo api check https://api.example.com
```

Communicate through structured issues in this repository using an installed and
authenticated GitHub CLI:

```sh
godo issue templates
godo issue add bug --title "..." \
  --field observed="..." --field expected="..." --field reproduce="..."
godo issue search "pagination" --state open
godo issue comment 12 --body "Confirmed"
```

The workflow is intentionally fixed to `nathanpls/godo` during the open alpha.

On Linux, `godo` can build Go programs or copy executables into persistent
`systemd --user` services and publish their local URLs to OpenCode agents:

```sh
godo service add ./docs \
  --name godo-docs \
  --additions 'Request Accept: text/markdown for canonical documentation'

godo service list
godo service update 1
godo service restart 1
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
├── channels/   # external communication integrations
├── core/       # reusable service building blocks
├── cli/
├── docs/
└── go.mod
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
