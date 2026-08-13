package main

import (
	"fmt"
	"io"
)

const rootHelp = `godo makes starting and running Go services fast.

Usage:
  godo <command> [options]

Commands:
  init       Initialize a minimal Go project
  add        Add a known godo package or database driver
  auth       Manage project API keys
  api        Inspect an API's agent-readiness contract
  issue      Communicate through nathanpls/godo GitHub issues
  service    Manage persistent local Go services
  db         Generate and run database migrations
  source     Inspect the godo source selected by this project
  agent      Publish local services to the global AGENTS.md

Run "godo <command> --help" for command-specific help.`

const initHelp = `Initialize a minimal Go project without adding dependencies.

Usage:
  godo init [directory] [--module <path>]

Arguments:
  directory           Target directory; defaults to the current directory

Options:
  --module <path>     Go module path; defaults to the target directory name

The target must be empty. The command creates go.mod, main.go, and .gitignore.`

const addHelp = `Add a known package to the current Go module without editing source files.

Usage:
  godo add <package>

Packages:
  http        github.com/nathanpls/godo/http
  orm         github.com/nathanpls/godo/orm
  id          github.com/nathanpls/godo/id
  lifecycle   github.com/nathanpls/godo/lifecycle
  password    github.com/nathanpls/godo/password
  validate    github.com/nathanpls/godo/validate
  ratelimit   github.com/nathanpls/godo/http/plugins/ratelimit
  apikey      github.com/nathanpls/godo/http/plugins/apikey
  agentapi    github.com/nathanpls/godo/http/plugins/agentapi
  idempotency github.com/nathanpls/godo/http/plugins/idempotency
  requestid   github.com/nathanpls/godo/http/plugins/requestid
  sqlite      modernc.org/sqlite
  postgres    github.com/jackc/pgx/v5/stdlib

The command runs go get from the module root and prints the relevant docs URL.`

const authHelp = `Manage bearer API keys for the current Go project.

Usage:
  godo auth <command>

Commands:
  init       Create .godo/auth.json and protect it from Git
  create     Create a key and print its secret once
  list       List non-secret key metadata
  revoke     Permanently revoke a key by ID

Run "godo auth <command> --help" for command-specific help.`

const authInitHelp = `Initialize project-local API key storage.

Usage:
  godo auth init

Creates .godo/auth.json with mode 0600 and .godo/.gitignore.`

const authCreateHelp = `Create a cryptographically random bearer API key.

Usage:
  godo auth create --name <name> [--scope <scope> ...]

Options:
  --name <name>    Human-readable key name
  --scope <scope>  Permission assigned to the key; repeat for multiple scopes

The secret is displayed once and only its SHA-256 hash is stored.`

const authListHelp = `List API key IDs, names, prefixes, and creation times.

Usage:
  godo auth list`

const authRevokeHelp = `Permanently revoke an API key by its numeric ID.

Usage:
  godo auth revoke <id>`

const apiHelp = `Inspect an HTTP API's agent discovery contract.

Usage:
  godo api check <base-url>

Commands:
  check    Validate discovery, OpenAPI, llms.txt, Markdown docs, and request IDs

Run "godo api check --help" for check details.`

const apiCheckHelp = `Validate an API's agent-readable discovery endpoints.

Usage:
  godo api check <base-url>

The check is read-only and validates /.well-known/godo.json, OpenAPI, llms.txt,
Markdown documentation, bearer metadata, and request ID response headers.`

const issueHelp = `Communicate through structured issues in nathanpls/godo.

Usage:
  godo issue <command> [options]

Commands:
  templates  List built-in issue templates
  template   Describe one template and its fields
  add        Create a structured issue
  list       List issues; open by default
  search     Search issues
  get        View one issue
  edit       Edit issue fields or metadata
  comment    Add a comment
  close      Close an issue
  reopen     Reopen an issue

GitHub operations require the gh CLI and an authenticated github.com account.
Every operation is fixed to the nathanpls/godo repository.`

const issueAddHelp = `Create a structured issue in nathanpls/godo.

Usage:
  godo issue add <template> --title <title> --field <name=value> [options]

Options:
  --field <name=value>  Template field; repeat for multiple fields
  --label <name>        Additional existing label; repeat for multiple labels
  --assignee <login>    Assignee; repeat for multiple assignees
  --dry-run             Render without contacting GitHub
  --json                Print structured output

Run "godo issue template <template>" to discover required fields.`

const issueListHelp = `List or search issues in nathanpls/godo.

Usage:
  godo issue list [options]
  godo issue search [query] [options]

Options:
  --state <open|closed|all>  Issue state; defaults to open
  --label <name>             Filter by label; repeat for multiple labels
  --author <login>           Filter by author
  --assignee <login>         Filter by assignee
  --limit <number>           Maximum results; defaults to 30
  --json                     Print stable JSON fields`

const issueGetHelp = `View one issue in nathanpls/godo.

Usage:
  godo issue get <number> [--comments] [--json]`

const issueEditHelp = `Edit an issue in nathanpls/godo.

Usage:
  godo issue edit <number> [options]

Options:
  --title <title>              Replace the title
  --field <name=value>         Replace a godo-managed template field
  --add-label <name>           Add a label
  --remove-label <name>        Remove a label
  --add-assignee <login>       Add an assignee
  --remove-assignee <login>    Remove an assignee
  --dry-run                    Render field changes without editing GitHub

Field edits preserve all text outside the godo-managed body block.`

const issueCommentHelp = `Comment on an issue in nathanpls/godo.

Usage:
  godo issue comment <number> --body <text>

Shell quoting may include newlines in the comment body.`

const issueCloseHelp = `Close an issue in nathanpls/godo.

Usage:
  godo issue close <number> [--comment <text>]`

const issueReopenHelp = `Reopen an issue in nathanpls/godo.

Usage:
  godo issue reopen <number> [--comment <text>]`

const serviceHelp = `Manage persistent local Go services through systemd --user.

Usage:
  godo service <command>

Commands:
  add       Build and start a Go service
  list      List managed services
  update    Rebuild and restart a service
  edit      Change a service name or agent additions
  remove    Stop and remove a service

Run "godo service <command> --help" for command-specific help.`

const serviceAddHelp = `Build a Go target and start it as a persistent local service.

Usage:
  godo service add <target> [--name <name>] [--port <port>] [--additions <text>]

Options:
  --name <name>          Human-readable service name
  --port <port>          Fixed port; defaults to a free port in 41000-41999
  --additions <text>     Usage instructions published to agents

Targets may be Go package directories, package paths, or .go files.`

const serviceListHelp = `List managed local services.

Usage:
  godo service list`

const serviceUpdateHelp = `Rebuild the original target and restart its service.

Usage:
  godo service update <id>`

const serviceEditHelp = `Change service metadata without rebuilding or restarting it.

Usage:
  godo service edit <id> [--name <name>] [--additions <text>]

Options:
  --name <name>          Replace the human-readable service name
  --additions <text>     Replace agent usage instructions; use "" to clear them

The registry and managed AGENTS.md block are updated immediately.`

const serviceRemoveHelp = `Stop, disable, and remove a managed service.

Usage:
  godo service remove <id>`

const dbHelp = `Generate and run compiler-checked SQLite or PostgreSQL migrations.

Usage:
  godo db <command>

Commands:
  init       Initialize db/godo, migrations, and the schema lock
  generate   Generate an up/down migration from registered models
  migrate    Apply pending migrations
  rollback   Roll back the latest applied migration
  status     Show pending and applied migrations

Run "godo db <command> --help" for command-specific help.`

const dbInitHelp = `Initialize a compiler-checked database schema program.

Usage:
  godo db init --dialect <sqlite|postgres>

Options:
  --dialect <name>    Database dialect: sqlite or postgres`

const dbGenerateHelp = `Compare registered models with schema.lock.json and generate SQL.

Usage:
  godo db generate <name> [--empty]

Options:
  --empty    Create an empty up/down pair for custom SQL; models must be unchanged`

const dbMigrateHelp = `Apply all pending migrations in version order.

Usage:
  godo db migrate`

const dbRollbackHelp = `Roll back the latest applied migration.

Usage:
  godo db rollback`

const dbStatusHelp = `Show pending, applied, modified, and missing migrations.

Usage:
  godo db status`

const sourceHelp = `Inspect the exact godo source selected by the current Go project.

Usage:
  godo source <package>
  godo source search <query> [options]

Commands:
  search     Search godo source code for a literal string

Arguments:
  package    Package relative to github.com/nathanpls/godo; use . for the module root

The package form prints its resolved source directory. Resolution follows the
current project's selected module version, workspace, and replace directives.`

const sourceSearchHelp = `Search the exact godo source selected by the current Go project.

Usage:
  godo source search <query> [--package <package>] [--context <lines>]

Options:
  --package <package>    Restrict the search to a godo package
  --context <lines>      Show surrounding lines; defaults to 0

The query is a case-sensitive literal string. Without context, results use
module-relative file:line output. Context output groups lines under each file.`

const agentHelp = `Synchronize managed local services with the global OpenCode AGENTS.md.

Usage:
  godo agent`

func isHelp(arguments []string) bool {
	return len(arguments) == 1 && (arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h")
}

func printHelp(output io.Writer, help string) error {
	_, err := fmt.Fprintln(output, help)
	return err
}
