package orm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// Insert adds a struct to its model table. model must be a non-nil pointer so
// an automatically generated integer primary key can be assigned.
func (db *DB) Insert(ctx context.Context, model any) error {
	if err := db.validate(); err != nil {
		return err
	}
	root, info, err := mutableModel(model)
	if err != nil {
		return err
	}

	fieldIndexes := make([]int, 0, len(info.fields))
	arguments := make([]any, 0, len(info.fields))
	autoOmitted := false
	for i, field := range info.fields {
		value, err := fieldValue(root, field, false)
		if err != nil {
			return err
		}
		if field.auto && value.IsZero() {
			autoOmitted = true
			continue
		}
		fieldIndexes = append(fieldIndexes, i)
		arguments = append(arguments, value.Interface())
	}

	table, _ := quoteIdentifier(info.table)
	query := "INSERT INTO " + table
	if len(fieldIndexes) == 0 {
		query += " DEFAULT VALUES"
	} else {
		columns := make([]string, len(fieldIndexes))
		placeholders := make([]string, len(fieldIndexes))
		for i, index := range fieldIndexes {
			columns[i], _ = quoteIdentifier(info.fields[index].column)
			placeholders[i] = db.dialect.placeholder(i + 1)
		}
		query += " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	}

	if autoOmitted && db.dialect == PostgreSQL {
		column, _ := quoteIdentifier(info.fields[info.autoField].column)
		var generated int64
		if err := db.runner.QueryRowContext(ctx, query+" RETURNING "+column, arguments...).Scan(&generated); err != nil {
			return fmt.Errorf("orm: insert %s: %w", info.table, err)
		}
		return setGeneratedID(root, info.fields[info.autoField], generated)
	}

	result, err := db.runner.ExecContext(ctx, query, arguments...)
	if err != nil {
		return fmt.Errorf("orm: insert %s: %w", info.table, err)
	}
	if autoOmitted {
		generated, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("orm: read generated ID for %s: %w", info.table, err)
		}
		return setGeneratedID(root, info.fields[info.autoField], generated)
	}
	return nil
}

// Update writes every non-primary field using the model's primary-key values.
func (db *DB) Update(ctx context.Context, model any) error {
	if err := db.validate(); err != nil {
		return err
	}
	root, info, err := modelValue(model)
	if err != nil {
		return err
	}
	if len(info.primary) == 0 {
		return fmt.Errorf("orm: update %s requires a primary key", info.table)
	}

	primary := make(map[int]bool, len(info.primary))
	for _, index := range info.primary {
		primary[index] = true
	}
	assignments := make([]string, 0, len(info.fields)-len(info.primary))
	arguments := make([]any, 0, len(info.fields))
	for i, field := range info.fields {
		if primary[i] {
			continue
		}
		value, err := fieldValue(root, field, false)
		if err != nil {
			return err
		}
		column, _ := quoteIdentifier(field.column)
		arguments = append(arguments, value.Interface())
		assignments = append(assignments, column+" = "+db.dialect.placeholder(len(arguments)))
	}
	if len(assignments) == 0 {
		return fmt.Errorf("orm: model %s has no fields to update", info.typ)
	}

	where, primaryArguments, err := primaryWhere(db.dialect, root, info, len(arguments))
	if err != nil {
		return err
	}
	arguments = append(arguments, primaryArguments...)
	table, _ := quoteIdentifier(info.table)
	result, err := db.runner.ExecContext(ctx, "UPDATE "+table+" SET "+strings.Join(assignments, ", ")+" WHERE "+where, arguments...)
	if err != nil {
		return fmt.Errorf("orm: update %s: %w", info.table, err)
	}
	return requireAffected(result, "update", info.table)
}

// Delete removes a row using the model's primary-key values.
func (db *DB) Delete(ctx context.Context, model any) error {
	if err := db.validate(); err != nil {
		return err
	}
	root, info, err := modelValue(model)
	if err != nil {
		return err
	}
	if len(info.primary) == 0 {
		return fmt.Errorf("orm: delete %s requires a primary key", info.table)
	}
	where, arguments, err := primaryWhere(db.dialect, root, info, 0)
	if err != nil {
		return err
	}
	table, _ := quoteIdentifier(info.table)
	result, err := db.runner.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+where, arguments...)
	if err != nil {
		return fmt.Errorf("orm: delete %s: %w", info.table, err)
	}
	return requireAffected(result, "delete", info.table)
}

func mutableModel(model any) (reflect.Value, *modelInfo, error) {
	if model == nil {
		return reflect.Value{}, nil, fmt.Errorf("orm: model must be a non-nil pointer to a struct")
	}
	value := reflect.ValueOf(model)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, nil, fmt.Errorf("orm: model must be a non-nil pointer to a struct")
	}
	info, err := modelForType(value.Elem().Type())
	return value.Elem(), info, err
}

func modelValue(model any) (reflect.Value, *modelInfo, error) {
	if model == nil {
		return reflect.Value{}, nil, fmt.Errorf("orm: model must be a struct or pointer to a struct")
	}
	value := reflect.ValueOf(model)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, nil, fmt.Errorf("orm: model must not be nil")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, nil, fmt.Errorf("orm: model must be a struct or pointer to a struct")
	}
	info, err := modelForType(value.Type())
	return value, info, err
}

func primaryWhere(dialect Dialect, root reflect.Value, info *modelInfo, offset int) (string, []any, error) {
	conditions := make([]string, len(info.primary))
	arguments := make([]any, len(info.primary))
	for i, index := range info.primary {
		field := info.fields[index]
		value, err := fieldValue(root, field, false)
		if err != nil {
			return "", nil, err
		}
		if value.IsZero() {
			return "", nil, fmt.Errorf("orm: primary key field %s must not be zero", field.goName)
		}
		column, _ := quoteIdentifier(field.column)
		conditions[i] = column + " = " + dialect.placeholder(offset+i+1)
		arguments[i] = value.Interface()
	}
	return strings.Join(conditions, " AND "), arguments, nil
}

func setGeneratedID(root reflect.Value, field modelField, generated int64) error {
	value, err := fieldValue(root, field, true)
	if err != nil {
		return err
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		value = value.Elem()
	}
	if value.Kind() >= reflect.Int && value.Kind() <= reflect.Int64 {
		if value.OverflowInt(generated) {
			return fmt.Errorf("orm: generated ID %d overflows %s", generated, value.Type())
		}
		value.SetInt(generated)
		return nil
	}
	if generated < 0 || value.OverflowUint(uint64(generated)) {
		return fmt.Errorf("orm: generated ID %d overflows %s", generated, value.Type())
	}
	value.SetUint(uint64(generated))
	return nil
}

func requireAffected(result interface{ RowsAffected() (int64, error) }, operation, table string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("orm: %s %s: read affected rows: %w", operation, table, err)
	}
	if rows == 0 {
		return fmt.Errorf("orm: %s %s: no row matched the primary key", operation, table)
	}
	return nil
}
