package dbtool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nathanpls/godo/core/orm"
)

// Config connects a schema program to its models and application-owned driver.
type Config struct {
	Dialect orm.Dialect
	Driver  string
	Source  string
	Models  []any
}

// Env returns an environment variable or fallback when it is empty.
func Env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// Main runs a generated database schema program.
func Main(config Config) {
	if err := Run(context.Background(), config, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "godo db:", err)
		os.Exit(1)
	}
}

// Run executes a schema-program command.
func Run(ctx context.Context, config Config, arguments []string, output io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("schema program must be run through godo db")
	}
	switch arguments[0] {
	case "schema":
		if len(arguments) != 1 {
			return errors.New("schema does not accept arguments")
		}
		schema, err := orm.Describe(config.Dialect, config.Models...)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(schema)
	case "migrate", "rollback", "status":
		if len(arguments) != 2 {
			return fmt.Errorf("%s requires the migrations directory", arguments[0])
		}
	default:
		return fmt.Errorf("unknown schema-program command %q", arguments[0])
	}

	if config.Driver == "" {
		return errors.New("database driver must not be empty")
	}
	if config.Source == "" {
		return errors.New("database source is empty")
	}
	if _, err := orm.ParseDialect(config.Dialect.String()); err != nil {
		return err
	}
	database, err := sql.Open(config.Driver, config.Source)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	switch arguments[0] {
	case "migrate":
		return migrate(ctx, database, config.Dialect, arguments[1], output)
	case "rollback":
		return rollback(ctx, database, config.Dialect, arguments[1], output)
	case "status":
		return status(ctx, database, config.Dialect, arguments[1], output)
	default:
		panic("unreachable")
	}
}
