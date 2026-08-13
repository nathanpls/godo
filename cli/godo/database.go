package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/nathanpls/godo/core/orm"
)

const schemaProgramSQLite = `package main

import (
	"github.com/nathanpls/godo/core/orm"
	"github.com/nathanpls/godo/core/orm/dbtool"

	_ "modernc.org/sqlite"

	// Import your model packages here.
)

func main() {
	dbtool.Main(dbtool.Config{
		Dialect: orm.SQLite,
		Driver:  "sqlite",
		Source:  dbtool.Env("DATABASE_URL", "db/app.db"),
		Models: []any{
			// models.User{},
		},
	})
}
`

const schemaProgramPostgres = `package main

import (
	"github.com/nathanpls/godo/core/orm"
	"github.com/nathanpls/godo/core/orm/dbtool"

	_ "github.com/jackc/pgx/v5/stdlib"

	// Import your model packages here.
)

func main() {
	dbtool.Main(dbtool.Config{
		Dialect: orm.PostgreSQL,
		Driver:  "pgx",
		Source:  dbtool.Env("DATABASE_URL", ""),
		Models: []any{
			// models.User{},
		},
	})
}
`

func (a *app) runDB(arguments []string) error {
	if len(arguments) == 0 || isHelp(arguments) {
		return printHelp(a.stdout, dbHelp)
	}
	switch arguments[0] {
	case "init":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, dbInitHelp)
		}
		dialect, err := parseDBInit(arguments[1:])
		if err != nil {
			return err
		}
		return a.initDB(dialect)
	case "generate":
		if isHelp(arguments[1:]) {
			return printHelp(a.stdout, dbGenerateHelp)
		}
		name, empty, err := parseDBGenerate(arguments[1:])
		if err != nil {
			return err
		}
		return a.generateDB(name, empty)
	case "migrate", "rollback", "status":
		if isHelp(arguments[1:]) {
			help := map[string]string{"migrate": dbMigrateHelp, "rollback": dbRollbackHelp, "status": dbStatusHelp}
			return printHelp(a.stdout, help[arguments[0]])
		}
		if len(arguments) != 1 {
			return fmt.Errorf("db %s does not accept arguments", arguments[0])
		}
		return a.runDBProgram(arguments[0])
	default:
		return fmt.Errorf("unknown db command %q", arguments[0])
	}
}

func parseDBInit(arguments []string) (orm.Dialect, error) {
	var value string
	for i := 0; i < len(arguments); i++ {
		name, inline, hasInline := strings.Cut(arguments[i], "=")
		if name != "--dialect" {
			return 0, fmt.Errorf("unknown db init option %q", arguments[i])
		}
		value = inline
		if !hasInline {
			i++
			if i >= len(arguments) {
				return 0, errors.New("--dialect requires a value")
			}
			value = arguments[i]
		}
	}
	if value == "" {
		return 0, errors.New("db init requires --dialect sqlite or --dialect postgres")
	}
	return orm.ParseDialect(value)
}

func parseDBGenerate(arguments []string) (string, bool, error) {
	var name string
	empty := false
	for _, argument := range arguments {
		switch argument {
		case "--empty":
			empty = true
		default:
			if strings.HasPrefix(argument, "-") {
				return "", false, fmt.Errorf("unknown db generate option %q", argument)
			}
			if name != "" {
				return "", false, errors.New("db generate accepts one migration name")
			}
			name = argument
		}
	}
	if !validDBMigrationName(name) {
		return "", false, errors.New("migration name must contain only letters, numbers, hyphens, or underscores")
	}
	return name, empty, nil
}

func validDBMigrationName(value string) bool {
	for _, character := range value {
		if character == '-' || character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character) {
			continue
		}
		return false
	}
	return value != ""
}

func (a *app) initDB(dialect orm.Dialect) error {
	root, err := projectRoot(a.cwd)
	if err != nil {
		return err
	}
	directory := filepath.Join(root, "db")
	programPath := filepath.Join(directory, "godo", "main.go")
	lockPath := filepath.Join(directory, "schema.lock.json")
	migrations := filepath.Join(directory, "migrations")
	for _, path := range []string{programPath, lockPath} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(migrations, 0o755); err != nil {
		return fmt.Errorf("create database directories: %w", err)
	}
	program := schemaProgramSQLite
	if dialect == orm.PostgreSQL {
		program = schemaProgramPostgres
	}
	if err := writeAtomic(programPath, []byte(program), 0o644); err != nil {
		return fmt.Errorf("write schema program: %w", err)
	}
	schema := orm.Schema{Version: orm.SchemaVersion, Dialect: dialect.String(), Tables: []orm.SchemaTable{}}
	if err := writeSchema(lockPath, schema); err != nil {
		_ = os.Remove(programPath)
		return err
	}
	if err := writeAtomic(filepath.Join(migrations, ".gitkeep"), nil, 0o644); err != nil {
		_ = os.Remove(programPath)
		_ = os.Remove(lockPath)
		return fmt.Errorf("initialize migrations directory: %w", err)
	}

	driver := "modernc.org/sqlite"
	if dialect == orm.PostgreSQL {
		driver = "github.com/jackc/pgx/v5/stdlib"
	}
	fmt.Fprintf(a.stdout, "Initialized %s database schema in %s\n", dialect.String(), directory)
	fmt.Fprintln(a.stdout, "Register models in db/godo.go, then run:")
	fmt.Fprintf(a.stdout, "  go get %s\n", driver)
	fmt.Fprintln(a.stdout, "  godo db generate <name>")
	return nil
}

func (a *app) generateDB(name string, empty bool) error {
	root, err := projectRoot(a.cwd)
	if err != nil {
		return err
	}
	paths := databasePaths(root)
	if err := requireDatabaseProject(paths); err != nil {
		return err
	}
	unlock, err := lockDatabaseProject(filepath.Join(root, "db", ".godo.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	version, err := nextMigrationVersion(paths.migrations)
	if err != nil {
		return err
	}
	upPath := filepath.Join(paths.migrations, fmt.Sprintf("%06d_%s.up.sql", version, name))
	downPath := filepath.Join(paths.migrations, fmt.Sprintf("%06d_%s.down.sql", version, name))
	current, err := exportSchema(root, paths.program)
	if err != nil {
		return err
	}
	previous, err := readSchema(paths.lock)
	if err != nil {
		return err
	}
	migration, err := orm.GenerateMigration(previous, current)
	if err != nil {
		return err
	}
	if empty {
		if len(migration.Up) != 0 {
			return errors.New("models changed; generate the model migration before creating an empty migration")
		}
		if err := writeMigrationPair(upPath, downPath, "", ""); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "Generated empty migration %06d_%s\n", version, name)
		return nil
	}

	if len(migration.Up) == 0 {
		fmt.Fprintln(a.stdout, "No model changes.")
		return nil
	}
	if err := writeMigrationPair(upPath, downPath, migrationSQL(migration.Up), migrationSQL(migration.Down)); err != nil {
		return err
	}
	if err := writeSchema(paths.lock, current); err != nil {
		_ = os.Remove(upPath)
		_ = os.Remove(downPath)
		return err
	}
	fmt.Fprintf(a.stdout, "Generated migration %06d_%s\n", version, name)
	return nil
}

func (a *app) runDBProgram(operation string) error {
	root, err := projectRoot(a.cwd)
	if err != nil {
		return err
	}
	paths := databasePaths(root)
	if err := requireDatabaseProject(paths); err != nil {
		return err
	}
	command := exec.Command("go", "run", paths.program, operation, paths.migrations)
	command.Dir = root
	command.Stdin = os.Stdin
	command.Stdout = a.stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("db %s: %w", operation, err)
	}
	return nil
}

type dbPaths struct {
	program    string
	lock       string
	migrations string
}

func databasePaths(root string) dbPaths {
	return dbPaths{
		program:    filepath.Join(root, "db", "godo", "main.go"),
		lock:       filepath.Join(root, "db", "schema.lock.json"),
		migrations: filepath.Join(root, "db", "migrations"),
	}
}

func lockDatabaseProject(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open database project lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock database project: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func requireDatabaseProject(paths dbPaths) error {
	for _, path := range []string{paths.program, paths.lock, paths.migrations} {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errors.New("database is not initialized; run godo db init --dialect <sqlite|postgres>")
			}
			return err
		}
	}
	return nil
}

func projectRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(filepath.Join(current, "go.mod"))
		if err == nil {
			if info.IsDir() {
				return "", fmt.Errorf("%s is a directory, expected a go.mod file", filepath.Join(current, "go.mod"))
			}
			return current, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect %s: %w", filepath.Join(current, "go.mod"), err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("could not find go.mod in this directory or its parents")
		}
		current = parent
	}
}

func exportSchema(root, program string) (orm.Schema, error) {
	command := exec.Command("go", "run", program, "schema")
	command.Dir = root
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return orm.Schema{}, fmt.Errorf("compile schema program: %w", err)
	}
	var schema orm.Schema
	decoder := json.NewDecoder(&output)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&schema); err != nil {
		return orm.Schema{}, fmt.Errorf("decode exported schema: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return orm.Schema{}, errors.New("schema program produced unexpected output")
	}
	return schema, nil
}

func readSchema(path string) (orm.Schema, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return orm.Schema{}, fmt.Errorf("read schema lock: %w", err)
	}
	var schema orm.Schema
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&schema); err != nil {
		return orm.Schema{}, fmt.Errorf("decode schema lock: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return orm.Schema{}, errors.New("schema lock contains unexpected data")
	}
	return schema, nil
}

func writeSchema(path string, schema orm.Schema) error {
	content, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("encode schema lock: %w", err)
	}
	content = append(content, '\n')
	if err := writeAtomic(path, content, 0o644); err != nil {
		return fmt.Errorf("write schema lock: %w", err)
	}
	return nil
}

func nextMigrationVersion(directory string) (int64, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, fmt.Errorf("read migrations: %w", err)
	}
	var greatest int64
	for _, entry := range entries {
		versionText, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			continue
		}
		version, err := strconv.ParseInt(versionText, 10, 64)
		if err == nil && version > greatest {
			greatest = version
		}
	}
	return greatest + 1, nil
}

func migrationSQL(statements []string) string {
	var result strings.Builder
	for _, statement := range statements {
		result.WriteString(statement)
		result.WriteString(";\n")
	}
	return result.String()
}

func writeMigrationPair(upPath, downPath, up, down string) error {
	for _, path := range []string{upPath, downPath} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("migration file %s already exists", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := writeAtomic(upPath, []byte(up), 0o644); err != nil {
		return fmt.Errorf("write up migration: %w", err)
	}
	if err := writeAtomic(downPath, []byte(down), 0o644); err != nil {
		_ = os.Remove(upPath)
		return fmt.Errorf("write down migration: %w", err)
	}
	return nil
}
