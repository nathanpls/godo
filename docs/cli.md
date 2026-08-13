# CLI

The `godo` CLI builds Go programs into persistent local services. Services start
with the Linux user session, restart after failures, and are published to agents
through the global OpenCode `AGENTS.md` file.

## Requirements

- Go 1.26 or newer
- Linux with a running `systemd --user` manager
- Applications that listen on the port supplied through the `PORT` environment
  variable

## Install

From the repository root:

```sh
./cli/install.sh
```

The script builds and installs `godo` to `$HOME/.local/bin/godo`. Make sure
`$HOME/.local/bin` is on `PATH`. Alternatively, use `go install ./cli/godo` and
add the Go binary directory, normally `$HOME/go/bin`, to `PATH`.

## Help

Every command group and operation has focused help:

```sh
godo --help
godo init --help
godo add --help
godo service --help
godo service add --help
godo db --help
godo db generate --help
```

## Initialize a project

Create a new minimal Go project:

```sh
godo init my-api
```

This creates `my-api/go.mod`, `my-api/main.go`, and `my-api/.gitignore`. The
directory must be empty. The module path defaults to the directory name; provide
an explicit publishable path when needed:

```sh
godo init my-api --module github.com/example/my-api
```

Initialize the current empty directory with:

```sh
godo init . --module github.com/example/my-api
```

The generated `main.go` contains only an empty `main` function. `godo init` does
not silently choose an application architecture or add dependencies.

## Add dependencies

Add a known package to the current Go module without modifying source files:

```sh
godo add http
godo add orm
godo add ratelimit
godo add apikey
godo add sqlite
godo add postgres
```

The command locates the nearest parent `go.mod`, runs `go get` from that module
root, and prints the relevant local documentation URL. Database drivers also
print the matching `godo db init` command.

Supported names map to:

| Name | Dependency |
|---|---|
| `http` | `github.com/nathanpls/godo/http` |
| `orm` | `github.com/nathanpls/godo/orm` |
| `ratelimit` | `github.com/nathanpls/godo/http/plugins/ratelimit` |
| `apikey` | `github.com/nathanpls/godo/http/plugins/apikey` |
| `sqlite` | `modernc.org/sqlite` |
| `postgres` | `github.com/jackc/pgx/v5/stdlib` |

`godo add` uses an explicit allowlist and does not accept arbitrary module paths.

## API keys

Initialize project-local API key storage:

```sh
godo auth init
```

Create, list, and revoke keys:

```sh
godo auth create --name opencode
godo auth list
godo auth revoke 1
```

Secrets are displayed once and only SHA-256 hashes are stored in the ignored
`.godo/auth.json` file. Key changes take effect without restarting applications
using the API key plugin. See the [API key documentation](/http/plugins/apikey)
for installation, request identity, and custom responses.

## Add a service

Pass a Go package directory, package path, or individual `.go` file:

```sh
godo service add ./docs \
  --name godo-docs \
  --additions 'Request Accept: text/markdown for canonical documentation'
```

This command:

1. Builds the target into a standalone binary.
2. Selects the first available port from `41000-41999`.
3. Installs and starts a `systemd --user` service.
4. Adds the service URL and usage note to the managed `<godo>` block in the
   global OpenCode `AGENTS.md`.

Set a specific port when needed:

```sh
godo service add ./docs --name godo-docs --port 8080
```

The target is resolved relative to the current directory when it is added. It
can therefore be rebuilt later regardless of the current shell directory.

## List services

```sh
godo service list
```

The output contains the stable ID, name, local URL, build target, and agent
instructions for each service. IDs start at `1`, increase monotonically, and are
not reused after removal.

## Update a service

Rebuild the original target and restart its service:

```sh
godo service update 1
```

The new binary is built before the running service is changed. If the updated
service cannot restart, godo restores and restarts the previous binary.

## Edit service metadata

Change the name or agent instructions without rebuilding or restarting the
service:

```sh
godo service edit 1 \
  --name "godo library docs" \
  --additions "Use Accept: text/markdown for agent-friendly documentation"
```

Clear additions with:

```sh
godo service edit 1 --additions ""
```

The registry and managed `<godo>` block in the global `AGENTS.md` are updated
immediately.

## Remove a service

```sh
godo service remove 1
```

This stops and disables the user service, removes its built files, updates the
registry, and removes it from agent discovery.

## Agent discovery

Service changes synchronize `~/.config/opencode/AGENTS.md` automatically. Run a
manual synchronization with:

```sh
godo agent
```

Only content between `<godo>` and `</godo>` is generated. Existing instructions
outside that block are preserved. A missing block is appended to the file. The
generated block also tells agents that the `godo` CLI is available and points
them to `godo --help` and nested command help.

## Application contract

Services receive their assigned port in `PORT`:

```go
port := os.Getenv("PORT")
if port == "" {
    port = "8080"
}

log.Fatal(http.ListenAndServe(":"+port, handler))
```

## Database migrations

Initialize a compiler-checked SQLite or PostgreSQL schema program:

```sh
godo db init --dialect sqlite
godo db init --dialect postgres
```

Register ORM models in `db/godo/main.go`, install the driver printed by `init`,
then generate and run migrations:

```sh
godo db generate create_users
godo db status
godo db migrate
godo db rollback
```

Use `godo db generate <name> --empty` for explicit custom or data migrations
when models have not changed. See the [ORM documentation](/orm) for schema
registration, generated files, supported automatic changes, and safety rules.

## Files and logs

- Registry: `$XDG_CONFIG_HOME/godo/services.json`, normally
  `~/.config/godo/services.json`
- Binaries: `$XDG_DATA_HOME/godo/services`, normally
  `~/.local/share/godo/services`
- User units: `$XDG_CONFIG_HOME/systemd/user`, normally
  `~/.config/systemd/user`
- Logs: `journalctl --user -u godo-<id>.service`

For example:

```sh
journalctl --user -u godo-1.service -f
```
