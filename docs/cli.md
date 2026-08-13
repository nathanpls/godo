# CLI

The `godo` CLI builds Go programs or copies executables into persistent local
services. Services start with the Linux user session, restart after failures,
and can be published to agents through the global OpenCode `AGENTS.md` file.

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
godo source --help
godo source search --help
godo issue --help
godo issue template bug --json
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

Use the explicit Discord template for a signal-aware bot starter:

```sh
godo init my-bot --module github.com/example/my-bot --template discord
cd my-bot
godo add discord
```

The template adds `.env.example` and a `main.go` that reads
`DISCORD_BOT_TOKEN`. It still leaves dependency selection to `godo add` and does
not load `.env` files automatically. See the [Discord documentation](/discord)
for Developer Portal and privileged intent setup.

## Add dependencies

Add a known package to the current Go module without modifying source files:

```sh
godo add http
godo add discord
godo add orm
godo add id
godo add lifecycle
godo add password
godo add validate
godo add ratelimit
godo add apikey
godo add agentapi
godo add idempotency
godo add requestid
godo add sqlite
godo add postgres
```

The command locates the nearest parent `go.mod`, runs `go get` from that module
root, and prints the relevant local documentation URL. Database drivers also
print the matching `godo db init` command.

Supported names map to:

| Name | Dependency |
|---|---|
| `http` | `github.com/nathanpls/godo/core/http` |
| `discord` | `github.com/nathanpls/godo/channels/discord` |
| `orm` | `github.com/nathanpls/godo/core/orm` |
| `id` | `github.com/nathanpls/godo/core/id` |
| `lifecycle` | `github.com/nathanpls/godo/core/lifecycle` |
| `password` | `github.com/nathanpls/godo/core/password` |
| `validate` | `github.com/nathanpls/godo/core/validate` |
| `ratelimit` | `github.com/nathanpls/godo/core/http/plugins/ratelimit` |
| `apikey` | `github.com/nathanpls/godo/core/http/plugins/apikey` |
| `agentapi` | `github.com/nathanpls/godo/core/http/plugins/agentapi` |
| `idempotency` | `github.com/nathanpls/godo/core/http/plugins/idempotency` |
| `requestid` | `github.com/nathanpls/godo/core/http/plugins/requestid` |
| `sqlite` | `modernc.org/sqlite` |
| `postgres` | `github.com/jackc/pgx/v5/stdlib` |

`godo add` uses an explicit allowlist and does not accept arbitrary module paths.

## Inspect godo source

Print the directory of a godo package selected by the current project:

```sh
godo source core/http/plugins/apikey
```

Search all godo Go source for a case-sensitive literal string:

```sh
godo source search "func (plugin *Plugin) middleware"
```

Restrict the search to one package and include surrounding lines:

```sh
godo source search "func (plugin *Plugin) middleware" \
  --package core/http/plugins/apikey \
  --context 5
```

Both forms locate the nearest parent `go.mod` and ask the Go tool to resolve the
exact `github.com/nathanpls/godo` source selected by that codebase. This follows
the selected module version, workspace, and `replace` directives without making
users locate or decode Go module-cache paths. The search runs dynamically on
regular `.go` files in that resolved source tree. Without context it returns
deterministic, module-relative `file:line:text` matches. Context output prints
each path once as `[file]`, marks matching lines with `>`, and uses `...` between
distant groups to avoid repeating paths in agent context. No match is successful
and prints no output.

Package arguments are relative to `github.com/nathanpls/godo`; use `.` for the
module root. The command does not add or upgrade godo when the module is absent
from the current build list.

## API keys

Initialize project-local API key storage:

```sh
godo auth init
```

Create, list, and revoke keys:

```sh
godo auth create --name opencode --scope plans:read --scope plans:write
godo auth list
godo auth revoke 1
```

Secrets are displayed once and only SHA-256 hashes are stored in the ignored
`.godo/auth.json` file. Key changes take effect without restarting applications
using the API key plugin. See the [API key documentation](/http/plugins/apikey)
for installation, request identity, and custom responses.

## Check an agent API

Validate an API's discovery contract without credentials or mutations:

```sh
godo api check https://api.example.com
```

The command checks same-origin `/.well-known/godo.json`, OpenAPI 3.1, `llms.txt`,
optional Markdown documentation, bearer metadata, and request ID headers. Links
and redirects cannot leave the supplied origin.

## Communicate through godo issues

`godo issue` is an open-alpha communication workflow fixed to the
`nathanpls/godo` GitHub repository. It intentionally cannot target another
repository. GitHub operations delegate to the GitHub CLI. Install `gh`, then
authenticate before using remote commands:

```sh
gh auth login
gh auth status
```

Template discovery is local and works without GitHub authentication:

```sh
godo issue templates
godo issue template bug
godo issue template feature --json
```

The built-in templates are `bug`, `feature`, `task`, and `investigation`. Each
template declares required and optional fields. Create a structured issue by
providing the required values:

```sh
godo issue add bug \
  --title "Cursor skips equal timestamps" \
  --field observed="An item disappears between pages" \
  --field expected="Every item appears exactly once" \
  --field reproduce="Create equal timestamps and request two pages"
```

Use `--dry-run` to inspect rendered Markdown without contacting GitHub. Add
`--json` to template discovery, dry-runs, lists, searches, and issue reads when
an agent needs structured output.

Contributors and maintainers can inspect and continue the conversation:

```sh
godo issue list --state open --json
godo issue search "cursor pagination" --label bug
godo issue get 12 --comments --json
godo issue comment 12 --body "Confirmed with the race detector."
godo issue close 12 --comment "Fixed in the latest release."
godo issue reopen 12
```

Update structured fields or ordinary GitHub metadata with:

```sh
godo issue edit 12 \
  --field reproduce="Run go test ./core/http -run TestCursor" \
  --add-label "help wanted"
```

Created bodies contain a godo-managed Markdown block. Field edits replace only
that block and preserve maintainer or contributor text outside it. Issues not
created from a supported godo template still allow title, label, and assignee
edits, but reject `--field` changes.

The first release supports only open and closed issue state. GitHub Projects,
milestones, arbitrary repositories, custom templates, and credential storage are
deliberately out of scope.

## Add a service

Pass a Go package directory, package path, individual `.go` file, or existing
executable:

```sh
godo service add ./docs \
  --name godo-docs \
  --additions 'Request Accept: text/markdown for canonical documentation'
```

This command:

1. Builds a Go target or copies an executable into managed storage.
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

Separate application arguments from godo options with `--`. Arguments are
stored and passed exactly without shell expansion:

```sh
godo service add ./bin/godex \
  --name godex-discord \
  --workdir "$PWD" \
  --env-file ~/.config/godex/discord.env \
  -- discord discord.json
```

An executable is managed as one file. Applications requiring sibling binaries
must provide a bundled executable or configure a stable absolute helper path.
For Godex, use `make bundle`; `make build` leaves `codex-app-server` as a sibling
that is not copied with `bin/godex`.

Relative application paths such as `discord.json` resolve against the runtime
working directory. Existing executable and package-path targets default to the
directory where `service add` runs. Go directories default to the target
directory, and `.go` files default to their parent. Use `--workdir` to make this
explicit. Godo does not guess which arguments are paths.

Managed services do not inherit the interactive shell environment. Use a
protected environment file for persistent secrets:

```sh
mkdir -p ~/.config/godex
chmod 700 ~/.config/godex
printf '%s\n' 'DISCORD_BOT_TOKEN=your-token' > ~/.config/godex/discord.env
chmod 600 ~/.config/godex/discord.env
```

The env-file path is resolved and stored as an absolute path; its contents are
never stored by godo. The file must be regular, non-symlink, and inaccessible to
group and others. `PORT` remains controlled by godo and overrides a value in the
file.

Use `--no-agent` when a service should remain in the registry and service list
but not appear in the generated `<godo>` block. Restore discovery later with
`godo service edit <id> --agent`.

## List services

```sh
godo service list
```

The output contains the stable ID, name, local URL, quoted command, agent
visibility, and agent instructions. IDs start at `1`, increase monotonically,
and are not reused after removal.

## Update a service

Rebuild the original target and restart its service:

```sh
godo service update 1
```

The new binary is built before the running service is changed. If the updated
service cannot restart, godo restores and restarts the previous binary.

Restart without rebuilding or recopying after changing env-file contents:

```sh
godo service restart 1
```

## Edit a service

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

Runtime settings and arguments can also be replaced:

```sh
godo service edit 1 \
  --workdir "$PWD" \
  --env-file ~/.config/godex/discord.env \
  -- discord discord.json
```

Use `--` with no following values to clear arguments, and `--env-file ""` to
clear the environment file. Runtime changes regenerate and restart the unit;
name, additions, and agent-visibility changes only update metadata.

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

Services configured with `--no-agent` are excluded. Only content between
`<godo>` and `</godo>` is generated. Existing instructions
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
