package orm

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// SchemaVersion is the current serialized schema format.
const SchemaVersion = 1

var reservedTables = map[string]bool{
	"_godo_migrations":     true,
	"_godo_migration_lock": true,
}

// Schema is a stable, serializable description of registered models.
type Schema struct {
	Version int           `json:"version"`
	Dialect string        `json:"dialect"`
	Tables  []SchemaTable `json:"tables"`
}

// SchemaTable describes one model table.
type SchemaTable struct {
	Name       string         `json:"name"`
	Columns    []SchemaColumn `json:"columns"`
	PrimaryKey []string       `json:"primary_key,omitempty"`
}

// SchemaColumn describes one model column.
type SchemaColumn struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"not_null,omitempty"`
	Unique  bool   `json:"unique,omitempty"`
	Auto    bool   `json:"auto,omitempty"`
}

// Migration contains generated forward and reverse SQL statements.
type Migration struct {
	Up   []string
	Down []string
}

// Describe returns a deterministic schema for models using dialect.
func Describe(dialect Dialect, models ...any) (Schema, error) {
	if err := dialect.validate(); err != nil {
		return Schema{}, err
	}
	result := Schema{Version: SchemaVersion, Dialect: dialect.String(), Tables: make([]SchemaTable, 0, len(models))}
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		info, err := modelFor(model)
		if err != nil {
			return Schema{}, err
		}
		if seen[info.table] {
			return Schema{}, fmt.Errorf("orm: multiple models map to table %q", info.table)
		}
		if reservedTables[info.table] {
			return Schema{}, fmt.Errorf("orm: table name %q is reserved", info.table)
		}
		seen[info.table] = true

		table := SchemaTable{Name: info.table, Columns: make([]SchemaColumn, 0, len(info.fields))}
		for _, field := range info.fields {
			columnType, err := dialect.columnType(field)
			if err != nil {
				return Schema{}, err
			}
			if dialect == SQLite && field.primary && !field.auto && columnType == "INTEGER" && len(info.primary) == 1 {
				columnType = "BIGINT"
			}
			table.Columns = append(table.Columns, SchemaColumn{
				Name:    field.column,
				Type:    columnType,
				NotNull: field.notNull || field.primary,
				Unique:  field.unique,
				Auto:    field.auto,
			})
		}
		for _, index := range info.primary {
			table.PrimaryKey = append(table.PrimaryKey, info.fields[index].column)
		}
		slices.SortFunc(table.Columns, func(a, b SchemaColumn) int { return strings.Compare(a.Name, b.Name) })
		result.Tables = append(result.Tables, table)
	}
	slices.SortFunc(result.Tables, func(a, b SchemaTable) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

// GenerateMigration compares previous with current and generates safe additive
// SQL. Destructive or ambiguous changes return an error.
func GenerateMigration(previous, current Schema) (Migration, error) {
	if err := validateSchema(current); err != nil {
		return Migration{}, err
	}
	if previous.Version == 0 && previous.Dialect == "" && len(previous.Tables) == 0 {
		previous = Schema{Version: SchemaVersion, Dialect: current.Dialect}
	}
	if err := validateSchema(previous); err != nil {
		return Migration{}, fmt.Errorf("orm: previous schema: %w", err)
	}
	if previous.Dialect != current.Dialect {
		return Migration{}, fmt.Errorf("orm: schema dialect changed from %s to %s", previous.Dialect, current.Dialect)
	}
	dialect, _ := ParseDialect(current.Dialect)

	oldTables := schemaTables(previous)
	newTables := schemaTables(current)
	for name := range oldTables {
		if _, exists := newTables[name]; !exists {
			return Migration{}, fmt.Errorf("orm: automatic migration would drop table %q", name)
		}
	}

	var migration Migration
	for _, table := range current.Tables {
		old, exists := oldTables[table.Name]
		if !exists {
			statement, err := schemaCreateTable(dialect, table)
			if err != nil {
				return Migration{}, err
			}
			migration.Up = append(migration.Up, statement)
			quoted, _ := quoteIdentifier(table.Name)
			migration.Down = append([]string{"DROP TABLE " + quoted}, migration.Down...)
			continue
		}
		if !slices.Equal(old.PrimaryKey, table.PrimaryKey) {
			return Migration{}, fmt.Errorf("orm: automatic migration cannot change the primary key for table %q", table.Name)
		}

		oldColumns := schemaColumns(old)
		newColumns := schemaColumns(table)
		for name := range oldColumns {
			if _, exists := newColumns[name]; !exists {
				return Migration{}, fmt.Errorf("orm: automatic migration would drop column %q.%q", table.Name, name)
			}
		}
		for _, column := range table.Columns {
			oldColumn, exists := oldColumns[column.Name]
			if exists {
				if oldColumn != column {
					return Migration{}, fmt.Errorf("orm: automatic migration cannot change column %q.%q", table.Name, column.Name)
				}
				continue
			}
			if column.NotNull || column.Unique || column.Auto || slices.Contains(table.PrimaryKey, column.Name) {
				return Migration{}, fmt.Errorf("orm: automatic migration can only add nullable unconstrained columns; %q.%q requires a manual migration", table.Name, column.Name)
			}
			tableName, _ := quoteIdentifier(table.Name)
			columnName, _ := quoteIdentifier(column.Name)
			migration.Up = append(migration.Up, "ALTER TABLE "+tableName+" ADD COLUMN "+columnName+" "+column.Type)
			migration.Down = append([]string{"ALTER TABLE " + tableName + " DROP COLUMN " + columnName}, migration.Down...)
		}
	}
	return migration, nil
}

// String returns the configuration name for a dialect.
func (d Dialect) String() string {
	switch d {
	case SQLite:
		return "sqlite"
	case PostgreSQL:
		return "postgres"
	default:
		return "unknown"
	}
}

// ParseDialect parses a dialect configuration name.
func ParseDialect(value string) (Dialect, error) {
	switch strings.ToLower(value) {
	case "sqlite", "sqlite3":
		return SQLite, nil
	case "postgres", "postgresql":
		return PostgreSQL, nil
	default:
		return 0, fmt.Errorf("orm: unsupported SQL dialect %q", value)
	}
}

func validateSchema(schema Schema) error {
	if schema.Version != SchemaVersion {
		return fmt.Errorf("unsupported schema version %d", schema.Version)
	}
	if _, err := ParseDialect(schema.Dialect); err != nil {
		return err
	}
	seenTables := make(map[string]bool, len(schema.Tables))
	for _, table := range schema.Tables {
		if _, err := quoteIdentifier(table.Name); err != nil || strings.Contains(table.Name, ".") {
			return fmt.Errorf("invalid table name %q", table.Name)
		}
		if seenTables[table.Name] {
			return fmt.Errorf("duplicate table %q", table.Name)
		}
		seenTables[table.Name] = true
		if reservedTables[table.Name] {
			return fmt.Errorf("table name %q is reserved", table.Name)
		}
		seenColumns := make(map[string]bool, len(table.Columns))
		for _, column := range table.Columns {
			if _, err := quoteIdentifier(column.Name); err != nil || strings.Contains(column.Name, ".") {
				return fmt.Errorf("invalid column name %q.%q", table.Name, column.Name)
			}
			if column.Type == "" || strings.ContainsAny(column.Type, ";\x00") {
				return fmt.Errorf("invalid column type for %q.%q", table.Name, column.Name)
			}
			if seenColumns[column.Name] {
				return fmt.Errorf("duplicate column %q.%q", table.Name, column.Name)
			}
			seenColumns[column.Name] = true
		}
		seenPrimary := make(map[string]bool, len(table.PrimaryKey))
		for _, primary := range table.PrimaryKey {
			if !seenColumns[primary] {
				return fmt.Errorf("primary key for %q references missing column %q", table.Name, primary)
			}
			if seenPrimary[primary] {
				return fmt.Errorf("primary key for %q repeats column %q", table.Name, primary)
			}
			seenPrimary[primary] = true
		}
		for _, column := range table.Columns {
			if !column.Auto {
				continue
			}
			if len(table.PrimaryKey) != 1 || table.PrimaryKey[0] != column.Name {
				return fmt.Errorf("auto column %q.%q must be the sole primary key", table.Name, column.Name)
			}
			validType := schema.Dialect == "sqlite" && column.Type == "INTEGER" || schema.Dialect == "postgres" && column.Type == "BIGSERIAL"
			if !validType {
				return fmt.Errorf("auto column %q.%q has invalid type %q", table.Name, column.Name, column.Type)
			}
		}
	}
	return nil
}

func schemaCreateTable(dialect Dialect, table SchemaTable) (string, error) {
	tableName, _ := quoteIdentifier(table.Name)
	singlePrimary := len(table.PrimaryKey) == 1
	definitions := make([]string, 0, len(table.Columns)+1)
	for _, column := range table.Columns {
		columnName, _ := quoteIdentifier(column.Name)
		definition := columnName + " " + column.Type
		if singlePrimary && table.PrimaryKey[0] == column.Name {
			definition += " PRIMARY KEY"
			if dialect == SQLite && column.Auto {
				definition += " AUTOINCREMENT"
			}
		}
		if column.NotNull && !(singlePrimary && table.PrimaryKey[0] == column.Name && column.Auto) {
			definition += " NOT NULL"
		}
		if column.Unique {
			definition += " UNIQUE"
		}
		definitions = append(definitions, definition)
	}
	if len(table.PrimaryKey) > 1 {
		primary := make([]string, len(table.PrimaryKey))
		for i, name := range table.PrimaryKey {
			primary[i], _ = quoteIdentifier(name)
		}
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primary, ", ")+")")
	}
	if len(definitions) == 0 {
		return "", errors.New("orm: table must have at least one column")
	}
	return "CREATE TABLE " + tableName + " (" + strings.Join(definitions, ", ") + ")", nil
}

func schemaTables(schema Schema) map[string]SchemaTable {
	result := make(map[string]SchemaTable, len(schema.Tables))
	for _, table := range schema.Tables {
		result[table.Name] = table
	}
	return result
}

func schemaColumns(table SchemaTable) map[string]SchemaColumn {
	result := make(map[string]SchemaColumn, len(table.Columns))
	for _, column := range table.Columns {
		result[column.Name] = column
	}
	return result
}
