package orm

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Dialect identifies database-specific SQL behavior.
type Dialect uint8

const (
	// SQLite generates SQL compatible with modern SQLite databases.
	SQLite Dialect = iota + 1
	// PostgreSQL generates SQL compatible with PostgreSQL databases.
	PostgreSQL
)

func (d Dialect) validate() error {
	if d != SQLite && d != PostgreSQL {
		return fmt.Errorf("orm: unsupported SQL dialect %d", d)
	}
	return nil
}

func (d Dialect) placeholder(position int) string {
	if d == PostgreSQL {
		return "$" + strconv.Itoa(position)
	}
	return "?"
}

func (d Dialect) columnType(field modelField) (string, error) {
	typeOf := field.typ
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}

	if field.auto {
		if !isInteger(typeOf.Kind()) {
			return "", fmt.Errorf("orm: auto field %s must be an integer", field.goName)
		}
		if d == SQLite {
			return "INTEGER", nil
		}
		switch typeOf.Kind() {
		case reflect.Int8, reflect.Int16:
			return "SMALLSERIAL", nil
		case reflect.Int32:
			return "SERIAL", nil
		default:
			return "BIGSERIAL", nil
		}
	}

	if known, ok := knownColumnType(d, typeOf); ok {
		return known, nil
	}
	switch typeOf.Kind() {
	case reflect.Bool:
		if d == SQLite {
			return "INTEGER", nil
		}
		return "BOOLEAN", nil
	case reflect.Int8, reflect.Int16:
		if d == SQLite {
			return "INTEGER", nil
		}
		return "SMALLINT", nil
	case reflect.Int32:
		if d == SQLite {
			return "INTEGER", nil
		}
		return "INTEGER", nil
	case reflect.Int, reflect.Int64:
		if d == SQLite {
			return "INTEGER", nil
		}
		return "BIGINT", nil
	case reflect.Uint8, reflect.Uint16:
		if d == SQLite {
			return "INTEGER", nil
		}
		return "INTEGER", nil
	case reflect.Uint, reflect.Uint32:
		if d == SQLite {
			return "INTEGER", nil
		}
		return "BIGINT", nil
	case reflect.Uint64:
		if d == SQLite {
			return "INTEGER", nil
		}
		return "NUMERIC(20)", nil
	case reflect.Float32:
		return "REAL", nil
	case reflect.Float64:
		if d == SQLite {
			return "REAL", nil
		}
		return "DOUBLE PRECISION", nil
	case reflect.String:
		return "TEXT", nil
	case reflect.Slice:
		if typeOf.Elem().Kind() == reflect.Uint8 {
			if d == SQLite {
				return "BLOB", nil
			}
			return "BYTEA", nil
		}
	}
	return "", fmt.Errorf("orm: field %s has unsupported schema type %s", field.goName, field.typ)
}

func knownColumnType(d Dialect, typeOf reflect.Type) (string, bool) {
	if typeOf == reflect.TypeFor[time.Time]() || typeOf == reflect.TypeFor[sql.NullTime]() {
		if d == SQLite {
			return "DATETIME", true
		}
		return "TIMESTAMPTZ", true
	}
	types := map[reflect.Type]string{
		reflect.TypeFor[sql.NullBool]():    "BOOLEAN",
		reflect.TypeFor[sql.NullByte]():    "SMALLINT",
		reflect.TypeFor[sql.NullFloat64](): "DOUBLE PRECISION",
		reflect.TypeFor[sql.NullInt16]():   "SMALLINT",
		reflect.TypeFor[sql.NullInt32]():   "INTEGER",
		reflect.TypeFor[sql.NullInt64]():   "BIGINT",
		reflect.TypeFor[sql.NullString]():  "TEXT",
	}
	result, ok := types[typeOf]
	if ok && d == SQLite && result != "TEXT" {
		if result == "DOUBLE PRECISION" {
			return "REAL", true
		}
		return "INTEGER", true
	}
	return result, ok
}

func quoteIdentifier(identifier string) (string, error) {
	if identifier == "" {
		return "", errors.New("orm: SQL identifier must not be empty")
	}
	parts := strings.Split(identifier, ".")
	for i, part := range parts {
		if !validIdentifier(part) {
			return "", fmt.Errorf("orm: invalid SQL identifier %q", identifier)
		}
		parts[i] = `"` + part + `"`
	}
	return strings.Join(parts, "."), nil
}

func validIdentifier(value string) bool {
	for i, character := range value {
		if character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || i > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return value != ""
}

func isInteger(kind reflect.Kind) bool {
	return kind >= reflect.Int && kind <= reflect.Int64 || kind >= reflect.Uint && kind <= reflect.Uint64
}
