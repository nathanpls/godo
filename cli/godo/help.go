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
  service    Manage persistent local Go services
  db         Generate and run database migrations
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
  ratelimit   github.com/nathanpls/godo/http/plugins/ratelimit
  sqlite      modernc.org/sqlite
  postgres    github.com/jackc/pgx/v5/stdlib

The command runs go get from the module root and prints the relevant docs URL.`

const serviceHelp = `Manage persistent local Go services through systemd --user.

Usage:
  godo service <command>

Commands:
  add       Build and start a Go service
  list      List managed services
  update    Rebuild and restart a service
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
