package dbtool

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/nathanpls/godo/core/orm"
)

type migration struct {
	version  int64
	name     string
	up       string
	down     string
	checksum string
	hasUp    bool
	hasDown  bool
}

type appliedMigration struct {
	version  int64
	name     string
	checksum string
}

func loadMigrations(directory string) ([]migration, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	byVersion := make(map[int64]*migration)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, name, direction, ok := parseMigrationName(entry.Name())
		if !ok {
			if strings.HasSuffix(entry.Name(), ".up.sql") || strings.HasSuffix(entry.Name(), ".down.sql") {
				return nil, fmt.Errorf("invalid migration filename %q; expected 000001_name.up.sql or 000001_name.down.sql", entry.Name())
			}
			continue
		}
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		current := byVersion[version]
		if current == nil {
			current = &migration{version: version, name: name}
			byVersion[version] = current
		} else if current.name != name {
			return nil, fmt.Errorf("migration version %06d has conflicting names", version)
		}
		switch direction {
		case "up":
			if current.hasUp {
				return nil, fmt.Errorf("migration %06d_%s has multiple up files", version, name)
			}
			current.up, current.hasUp = string(content), true
		case "down":
			if current.hasDown {
				return nil, fmt.Errorf("migration %06d_%s has multiple down files", version, name)
			}
			current.down, current.hasDown = string(content), true
		}
	}

	result := make([]migration, 0, len(byVersion))
	for _, current := range byVersion {
		if !current.hasUp || !current.hasDown {
			return nil, fmt.Errorf("migration %06d_%s requires matching up and down files", current.version, current.name)
		}
		hash := sha256.New()
		_, _ = io.WriteString(hash, current.up)
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, current.down)
		current.checksum = hex.EncodeToString(hash.Sum(nil))
		result = append(result, *current)
	}
	slices.SortFunc(result, func(a, b migration) int {
		if a.version < b.version {
			return -1
		}
		if a.version > b.version {
			return 1
		}
		return 0
	})
	return result, nil
}

func parseMigrationName(filename string) (int64, string, string, bool) {
	direction := ""
	switch {
	case strings.HasSuffix(filename, ".up.sql"):
		direction = "up"
		filename = strings.TrimSuffix(filename, ".up.sql")
	case strings.HasSuffix(filename, ".down.sql"):
		direction = "down"
		filename = strings.TrimSuffix(filename, ".down.sql")
	default:
		return 0, "", "", false
	}
	versionText, name, found := strings.Cut(filename, "_")
	if !found || len(versionText) < 6 || !validMigrationName(name) {
		return 0, "", "", false
	}
	version, err := strconv.ParseInt(versionText, 10, 64)
	if err != nil || version < 1 {
		return 0, "", "", false
	}
	return version, name, direction, true
}

func validMigrationName(name string) bool {
	for _, character := range name {
		if character == '_' || character == '-' || unicode.IsLetter(character) || unicode.IsDigit(character) {
			continue
		}
		return false
	}
	return name != ""
}

func migrate(ctx context.Context, database *sql.DB, dialect orm.Dialect, directory string, output io.Writer) error {
	migrations, err := loadMigrations(directory)
	if err != nil {
		return err
	}
	if err := ensureMigrationTable(ctx, database, dialect); err != nil {
		return err
	}
	tx, err := lockedMigrationTx(ctx, database, dialect)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	applied, err := readApplied(ctx, tx)
	if err != nil {
		return err
	}
	if err := validateMigrationHistory(migrations, applied); err != nil {
		return err
	}

	appliedNow := make([]migration, 0)
	for _, current := range migrations {
		if _, exists := applied[current.version]; exists {
			continue
		}
		if strings.TrimSpace(current.up) == "" {
			return fmt.Errorf("migration %06d_%s has empty up SQL", current.version, current.name)
		}
		if err := applyMigration(ctx, tx, dialect, current); err != nil {
			return err
		}
		appliedNow = append(appliedNow, current)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	for _, current := range appliedNow {
		fmt.Fprintf(output, "Applied %06d_%s\n", current.version, current.name)
	}
	if len(appliedNow) == 0 {
		fmt.Fprintln(output, "No pending migrations.")
	}
	return nil
}

func rollback(ctx context.Context, database *sql.DB, dialect orm.Dialect, directory string, output io.Writer) error {
	migrations, err := loadMigrations(directory)
	if err != nil {
		return err
	}
	if err := ensureMigrationTable(ctx, database, dialect); err != nil {
		return err
	}
	tx, err := lockedMigrationTx(ctx, database, dialect)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	applied, err := readApplied(ctx, tx)
	if err != nil {
		return err
	}
	if err := validateMigrationHistory(migrations, applied); err != nil {
		return err
	}
	if len(applied) == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit rollback check: %w", err)
		}
		fmt.Fprintln(output, "No applied migrations.")
		return nil
	}

	var latest appliedMigration
	for _, candidate := range applied {
		if candidate.version > latest.version {
			latest = candidate
		}
	}
	var current migration
	for _, candidate := range migrations {
		if candidate.version == latest.version {
			current = candidate
			break
		}
	}
	if strings.TrimSpace(current.down) == "" {
		return fmt.Errorf("migration %06d_%s has empty down SQL", current.version, current.name)
	}
	if err := rollbackMigration(ctx, tx, dialect, current); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rollback %06d_%s: %w", current.version, current.name, err)
	}
	fmt.Fprintf(output, "Rolled back %06d_%s\n", current.version, current.name)
	return nil
}

func status(ctx context.Context, database *sql.DB, dialect orm.Dialect, directory string, output io.Writer) error {
	migrations, err := loadMigrations(directory)
	if err != nil {
		return err
	}
	if err := ensureMigrationTable(ctx, database, dialect); err != nil {
		return err
	}
	applied, err := readApplied(ctx, database)
	if err != nil {
		return err
	}
	if len(migrations) == 0 && len(applied) == 0 {
		fmt.Fprintln(output, "No migrations.")
		return nil
	}

	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "VERSION\tNAME\tSTATUS")
	seen := make(map[int64]bool, len(migrations))
	for _, current := range migrations {
		state := "pending"
		if stored, exists := applied[current.version]; exists {
			state = "applied"
			if stored.name != current.name || stored.checksum != current.checksum {
				state = "modified"
			}
		}
		fmt.Fprintf(writer, "%06d\t%s\t%s\n", current.version, current.name, state)
		seen[current.version] = true
	}
	versions := make([]int64, 0)
	for version := range applied {
		if !seen[version] {
			versions = append(versions, version)
		}
	}
	slices.Sort(versions)
	for _, version := range versions {
		fmt.Fprintf(writer, "%06d\t%s\tmissing\n", version, applied[version].name)
	}
	return writer.Flush()
}

func ensureMigrationTable(ctx context.Context, database *sql.DB, dialect orm.Dialect) error {
	timestampType := "DATETIME"
	if dialect == orm.PostgreSQL {
		timestampType = "TIMESTAMPTZ"
	}
	statement := `CREATE TABLE IF NOT EXISTS "_godo_migrations" (` +
		`"version" BIGINT PRIMARY KEY, ` +
		`"name" TEXT NOT NULL, ` +
		`"checksum" TEXT NOT NULL, ` +
		`"applied_at" ` + timestampType + ` NOT NULL)`
	if _, err := database.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	return nil
}

type queryContextRunner interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readApplied(ctx context.Context, database queryContextRunner) (map[int64]appliedMigration, error) {
	rows, err := database.QueryContext(ctx, `SELECT "version", "name", "checksum" FROM "_godo_migrations" ORDER BY "version"`)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]appliedMigration)
	for rows.Next() {
		var current appliedMigration
		if err := rows.Scan(&current.version, &current.name, &current.checksum); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		result[current.version] = current
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	return result, nil
}

func validateMigrationHistory(migrations []migration, applied map[int64]appliedMigration) error {
	available := make(map[int64]migration, len(migrations))
	pendingSeen := false
	for _, current := range migrations {
		available[current.version] = current
		stored, exists := applied[current.version]
		if !exists {
			pendingSeen = true
			continue
		}
		if pendingSeen {
			return fmt.Errorf("applied migration %06d_%s follows a pending migration", current.version, stored.name)
		}
		if stored.name != current.name || stored.checksum != current.checksum {
			return fmt.Errorf("applied migration %06d_%s was modified", current.version, stored.name)
		}
	}
	for version, stored := range applied {
		if _, exists := available[version]; !exists {
			return fmt.Errorf("applied migration %06d_%s is missing from disk", version, stored.name)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, tx *sql.Tx, dialect orm.Dialect, current migration) error {
	if _, err := tx.ExecContext(ctx, current.up); err != nil {
		return fmt.Errorf("apply migration %06d_%s: %w", current.version, current.name, err)
	}
	query := `INSERT INTO "_godo_migrations" ("version", "name", "checksum", "applied_at") VALUES (`
	if dialect == orm.PostgreSQL {
		query += "$1, $2, $3, CURRENT_TIMESTAMP)"
	} else {
		query += "?, ?, ?, CURRENT_TIMESTAMP)"
	}
	if _, err := tx.ExecContext(ctx, query, current.version, current.name, current.checksum); err != nil {
		return fmt.Errorf("record migration %06d_%s: %w", current.version, current.name, err)
	}
	return nil
}

func rollbackMigration(ctx context.Context, tx *sql.Tx, dialect orm.Dialect, current migration) error {
	if _, err := tx.ExecContext(ctx, current.down); err != nil {
		return fmt.Errorf("roll back migration %06d_%s: %w", current.version, current.name, err)
	}
	query := `DELETE FROM "_godo_migrations" WHERE "version" = ?`
	if dialect == orm.PostgreSQL {
		query = `DELETE FROM "_godo_migrations" WHERE "version" = $1`
	}
	result, err := tx.ExecContext(ctx, query, current.version)
	if err != nil {
		return fmt.Errorf("remove migration record %06d_%s: %w", current.version, current.name, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read removed migration record %06d_%s: %w", current.version, current.name, err)
	}
	if rows != 1 {
		return fmt.Errorf("remove migration record %06d_%s: expected one row, removed %d", current.version, current.name, rows)
	}
	return nil
}

func lockedMigrationTx(ctx context.Context, database *sql.DB, dialect orm.Dialect) (*sql.Tx, error) {
	if dialect == orm.SQLite {
		if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS "_godo_migration_lock" ("id" INTEGER PRIMARY KEY)`); err != nil {
			return nil, fmt.Errorf("create migration lock: %w", err)
		}
		if _, err := database.ExecContext(ctx, `INSERT OR IGNORE INTO "_godo_migration_lock" ("id") VALUES (1)`); err != nil {
			return nil, fmt.Errorf("initialize migration lock: %w", err)
		}
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin migration transaction: %w", err)
	}
	if dialect == orm.PostgreSQL {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1735354211)`); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("lock migrations: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE "_godo_migration_lock" SET "id" = "id" WHERE "id" = 1`); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("lock migrations: %w", err)
		}
	}
	return tx, nil
}
