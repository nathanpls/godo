package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanpls/godo/core/orm"
)

func TestParseDBInit(t *testing.T) {
	dialect, err := parseDBInit([]string{"--dialect=postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if dialect != orm.PostgreSQL {
		t.Fatalf("dialect = %v, want PostgreSQL", dialect)
	}
	if _, err := parseDBInit(nil); err == nil {
		t.Fatal("missing dialect was accepted")
	}
}

func TestParseDBGenerate(t *testing.T) {
	name, empty, err := parseDBGenerate([]string{"create_users", "--empty"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "create_users" || !empty {
		t.Fatalf("parsed = %q, %t", name, empty)
	}
	if _, _, err := parseDBGenerate([]string{"invalid name"}); err == nil {
		t.Fatal("invalid migration name was accepted")
	}
}

func TestInitDB(t *testing.T) {
	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "go.mod"), "module example.com/app\n\ngo 1.26.0\n")
	var output strings.Builder
	application := &app{cwd: directory, stdout: &output}

	if err := application.initDB(orm.SQLite); err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(filepath.Join(directory, "db", "godo", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(program), "Dialect: orm.SQLite") || !strings.Contains(string(program), `_ "modernc.org/sqlite"`) {
		t.Fatalf("generated program:\n%s", program)
	}
	schema, err := readSchema(filepath.Join(directory, "db", "schema.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if schema.Dialect != "sqlite" || len(schema.Tables) != 0 {
		t.Fatalf("schema = %+v", schema)
	}
	if err := application.initDB(orm.SQLite); err == nil {
		t.Fatal("second initialization succeeded")
	}
}

func TestGenerateEmptyMigration(t *testing.T) {
	directory := t.TempDir()
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "go.mod"), "module example.com/app\n\ngo 1.26.0\n\nrequire github.com/nathanpls/godo v0.0.0\n\nreplace github.com/nathanpls/godo => "+repository+"\n")
	application := &app{cwd: directory, stdout: &strings.Builder{}}
	if err := application.initDB(orm.PostgreSQL); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "db", "godo", "main.go"), `package main

import (
	"github.com/nathanpls/godo/core/orm"
	"github.com/nathanpls/godo/core/orm/dbtool"
)

func main() {
	dbtool.Main(dbtool.Config{Dialect: orm.PostgreSQL})
}
`)
	if err := application.generateDB("custom_change", true); err != nil {
		t.Fatal(err)
	}
	up := filepath.Join(directory, "db", "migrations", "000001_custom_change.up.sql")
	down := filepath.Join(directory, "db", "migrations", "000001_custom_change.down.sql")
	if _, err := os.Stat(up); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(down); err != nil {
		t.Fatal(err)
	}
	if version, err := nextMigrationVersion(filepath.Dir(up)); err != nil || version != 2 {
		t.Fatalf("next version = %d, %v", version, err)
	}
	schema, err := readSchema(filepath.Join(directory, "db", "schema.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Tables) != 0 {
		t.Fatalf("empty migration advanced schema lock: %+v", schema)
	}
}

func TestGenerateModelMigration(t *testing.T) {
	directory := t.TempDir()
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "go.mod"), "module example.com/app\n\ngo 1.26.0\n\nrequire github.com/nathanpls/godo v0.0.0\n\nreplace github.com/nathanpls/godo => "+repository+"\n")
	application := &app{cwd: directory, stdout: &strings.Builder{}}
	if err := application.initDB(orm.SQLite); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "db", "godo", "main.go"), `package main

import (
	"github.com/nathanpls/godo/core/orm"
	"github.com/nathanpls/godo/core/orm/dbtool"
)

type User struct {
	ID    int64
	Email string `+"`orm:\"unique,notnull\"`"+`
}

func main() {
	dbtool.Main(dbtool.Config{Dialect: orm.SQLite, Models: []any{User{}}})
}
`)
	if err := application.generateDB("create_users", false); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile(filepath.Join(directory, "db", "migrations", "000001_create_users.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(up), `CREATE TABLE "users"`) || !strings.Contains(string(up), `"email" TEXT NOT NULL UNIQUE`) {
		t.Fatalf("generated up migration:\n%s", up)
	}
	down, err := os.ReadFile(filepath.Join(directory, "db", "migrations", "000001_create_users.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(down) != "DROP TABLE \"users\";\n" {
		t.Fatalf("generated down migration: %s", down)
	}
	schema, err := readSchema(filepath.Join(directory, "db", "schema.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "users" {
		t.Fatalf("schema lock = %+v", schema)
	}
}

func TestProjectRootFromChildDirectory(t *testing.T) {
	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "go.mod"), "module example.com/app\n")
	child := filepath.Join(directory, "internal", "models")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := projectRoot(child)
	if err != nil {
		t.Fatal(err)
	}
	if root != directory {
		t.Fatalf("root = %s, want %s", root, directory)
	}
}
