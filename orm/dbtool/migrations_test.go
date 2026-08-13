package dbtool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMigrations(t *testing.T) {
	directory := t.TempDir()
	writeMigrationFile(t, directory, "000002_add_email.up.sql", "ALTER TABLE users ADD email TEXT;\n")
	writeMigrationFile(t, directory, "000002_add_email.down.sql", "ALTER TABLE users DROP email;\n")
	writeMigrationFile(t, directory, "000001_create_users.up.sql", "CREATE TABLE users (id BIGINT);\n")
	writeMigrationFile(t, directory, "000001_create_users.down.sql", "DROP TABLE users;\n")
	writeMigrationFile(t, directory, "README.md", "ignored")

	migrations, err := loadMigrations(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 || migrations[0].version != 1 || migrations[1].version != 2 {
		t.Fatalf("migrations = %+v", migrations)
	}
	if migrations[0].checksum == "" || migrations[1].checksum == "" {
		t.Fatal("migration checksums were not generated")
	}
}

func TestLoadMigrationsRequiresPair(t *testing.T) {
	directory := t.TempDir()
	writeMigrationFile(t, directory, "000001_create_users.up.sql", "SELECT 1;")
	if _, err := loadMigrations(directory); err == nil {
		t.Fatal("unpaired migration was accepted")
	}
}

func TestLoadMigrationsRejectsMalformedSQLFilename(t *testing.T) {
	directory := t.TempDir()
	writeMigrationFile(t, directory, "00001_users.up.sql", "SELECT 1;")
	if _, err := loadMigrations(directory); err == nil {
		t.Fatal("malformed SQL migration filename was ignored")
	}
}

func TestParseMigrationName(t *testing.T) {
	version, name, direction, ok := parseMigrationName("000042_add_users.up.sql")
	if !ok || version != 42 || name != "add_users" || direction != "up" {
		t.Fatalf("parsed = %d, %q, %q, %t", version, name, direction, ok)
	}
	if _, _, _, ok := parseMigrationName("42_bad.up.sql"); ok {
		t.Fatal("short migration version was accepted")
	}
}

func TestValidateMigrationHistory(t *testing.T) {
	migrations := []migration{{version: 1, name: "users", checksum: "new"}}
	applied := map[int64]appliedMigration{1: {version: 1, name: "users", checksum: "old"}}
	if err := validateMigrationHistory(migrations, applied); err == nil {
		t.Fatal("modified migration was accepted")
	}
}

func TestValidateMigrationHistoryRequiresPrefix(t *testing.T) {
	migrations := []migration{
		{version: 1, name: "one", checksum: "one"},
		{version: 2, name: "two", checksum: "two"},
	}
	applied := map[int64]appliedMigration{2: {version: 2, name: "two", checksum: "two"}}
	if err := validateMigrationHistory(migrations, applied); err == nil {
		t.Fatal("non-prefix migration history was accepted")
	}
}

func writeMigrationFile(t *testing.T, directory, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
