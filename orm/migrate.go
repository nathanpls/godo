package orm

import (
	"context"
	"fmt"
	"strings"
)

// Migrate creates tables that do not exist. It does not alter existing tables.
func (db *DB) Migrate(ctx context.Context, models ...any) error {
	if err := db.validate(); err != nil {
		return err
	}
	for _, model := range models {
		info, err := modelFor(model)
		if err != nil {
			return err
		}
		statement, err := createTableSQL(db.dialect, info)
		if err != nil {
			return err
		}
		if _, err := db.runner.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("orm: migrate %s: %w", info.table, err)
		}
	}
	return nil
}

func createTableSQL(dialect Dialect, info *modelInfo) (string, error) {
	if err := dialect.validate(); err != nil {
		return "", err
	}
	table, _ := quoteIdentifier(info.table)
	definitions := make([]string, 0, len(info.fields)+1)
	singlePrimary := len(info.primary) == 1
	for _, field := range info.fields {
		column, _ := quoteIdentifier(field.column)
		columnType, err := dialect.columnType(field)
		if err != nil {
			return "", err
		}
		definition := column + " " + columnType
		if singlePrimary && field.primary {
			definition += " PRIMARY KEY"
			if field.auto && dialect == SQLite {
				definition += " AUTOINCREMENT"
			}
		}
		if field.notNull && !(singlePrimary && field.primary) {
			definition += " NOT NULL"
		}
		if field.unique {
			definition += " UNIQUE"
		}
		definitions = append(definitions, definition)
	}
	if len(info.primary) > 1 {
		columns := make([]string, len(info.primary))
		for i, index := range info.primary {
			columns[i], _ = quoteIdentifier(info.fields[index].column)
		}
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(columns, ", ")+")")
	}
	return "CREATE TABLE IF NOT EXISTS " + table + " (" + strings.Join(definitions, ", ") + ")", nil
}
