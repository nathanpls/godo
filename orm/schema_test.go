package orm

import (
	"reflect"
	"testing"
)

func TestDescribeSchemaIsDeterministic(t *testing.T) {
	schema, err := Describe(PostgreSQL, testUser{})
	if err != nil {
		t.Fatal(err)
	}
	if schema.Version != SchemaVersion || schema.Dialect != "postgres" || len(schema.Tables) != 1 {
		t.Fatalf("schema = %+v", schema)
	}
	columns := schema.Tables[0].Columns
	if !reflect.DeepEqual(columnNames(columns), []string{"account_id", "created_at", "data", "email", "id", "nickname"}) {
		t.Fatalf("columns = %+v", columns)
	}
}

func TestGenerateInitialMigration(t *testing.T) {
	current, err := Describe(SQLite, testUser{})
	if err != nil {
		t.Fatal(err)
	}
	migration, err := GenerateMigration(Schema{}, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(migration.Up) != 1 || len(migration.Down) != 1 {
		t.Fatalf("migration = %+v", migration)
	}
	if migration.Down[0] != `DROP TABLE "test_users"` {
		t.Fatalf("down = %q", migration.Down[0])
	}
}

func TestGenerateAddedNullableColumn(t *testing.T) {
	previous := Schema{Version: SchemaVersion, Dialect: "postgres", Tables: []SchemaTable{{
		Name: "users", Columns: []SchemaColumn{{Name: "id", Type: "BIGSERIAL", NotNull: true, Auto: true}}, PrimaryKey: []string{"id"},
	}}}
	current := previous
	current.Tables = []SchemaTable{{
		Name: "users",
		Columns: []SchemaColumn{
			{Name: "email", Type: "TEXT"},
			{Name: "id", Type: "BIGSERIAL", NotNull: true, Auto: true},
		},
		PrimaryKey: []string{"id"},
	}}
	migration, err := GenerateMigration(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(migration.Up, []string{`ALTER TABLE "users" ADD COLUMN "email" TEXT`}) {
		t.Fatalf("up = %#v", migration.Up)
	}
	if !reflect.DeepEqual(migration.Down, []string{`ALTER TABLE "users" DROP COLUMN "email"`}) {
		t.Fatalf("down = %#v", migration.Down)
	}
}

func TestGenerateRejectsDestructiveChanges(t *testing.T) {
	previous := Schema{Version: SchemaVersion, Dialect: "sqlite", Tables: []SchemaTable{{Name: "users", Columns: []SchemaColumn{{Name: "name", Type: "TEXT"}}}}}
	current := Schema{Version: SchemaVersion, Dialect: "sqlite"}
	if _, err := GenerateMigration(previous, current); err == nil {
		t.Fatal("table removal was accepted")
	}

	current = previous
	current.Tables = []SchemaTable{{Name: "users", Columns: []SchemaColumn{{Name: "name", Type: "INTEGER"}}}}
	if _, err := GenerateMigration(previous, current); err == nil {
		t.Fatal("column type change was accepted")
	}
}

func TestSchemaRejectsReservedAndInvalidAutoFields(t *testing.T) {
	reserved := Schema{Version: SchemaVersion, Dialect: "sqlite", Tables: []SchemaTable{{Name: "_godo_migrations", Columns: []SchemaColumn{{Name: "id", Type: "INTEGER"}}}}}
	if _, err := GenerateMigration(Schema{}, reserved); err == nil {
		t.Fatal("reserved migration table was accepted")
	}

	invalidAuto := Schema{Version: SchemaVersion, Dialect: "sqlite", Tables: []SchemaTable{{Name: "users", Columns: []SchemaColumn{{Name: "id", Type: "TEXT", Auto: true}}, PrimaryKey: []string{"id"}}}}
	if _, err := GenerateMigration(Schema{}, invalidAuto); err == nil {
		t.Fatal("invalid auto type was accepted")
	}

	duplicatePrimary := Schema{Version: SchemaVersion, Dialect: "postgres", Tables: []SchemaTable{{Name: "users", Columns: []SchemaColumn{{Name: "id", Type: "BIGINT"}}, PrimaryKey: []string{"id", "id"}}}}
	if _, err := GenerateMigration(Schema{}, duplicatePrimary); err == nil {
		t.Fatal("duplicate primary-key column was accepted")
	}
}

func columnNames(columns []SchemaColumn) []string {
	result := make([]string, len(columns))
	for i, column := range columns {
		result[i] = column.Name
	}
	return result
}
