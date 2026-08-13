package orm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Operator is a safe SQL comparison operator.
type Operator string

const (
	Equal              Operator = "="
	NotEqual           Operator = "<>"
	LessThan           Operator = "<"
	LessThanOrEqual    Operator = "<="
	GreaterThan        Operator = ">"
	GreaterThanOrEqual Operator = ">="
	Like               Operator = "LIKE"
)

// Direction is a SQL ordering direction.
type Direction string

const (
	Ascending  Direction = "ASC"
	Descending Direction = "DESC"
)

// Option configures a Find or First query.
type Option interface {
	apply(*queryOptions) error
}

type optionFunc func(*queryOptions) error

func (option optionFunc) apply(query *queryOptions) error { return option(query) }

type condition struct {
	column   string
	operator Operator
	value    any
	values   []any
	in       bool
	not      bool
	null     bool
}

type ordering struct {
	column    string
	direction Direction
}

type queryOptions struct {
	conditions []condition
	order      []ordering
	limit      int
	offset     int
	limitSet   bool
	offsetSet  bool
}

// Where adds an AND comparison. Nil with Equal or NotEqual generates IS NULL
// or IS NOT NULL.
func Where(column string, operator Operator, value any) Option {
	return optionFunc(func(query *queryOptions) error {
		if !validOperator(operator) {
			return fmt.Errorf("orm: invalid comparison operator %q", operator)
		}
		query.conditions = append(query.conditions, condition{column: column, operator: operator, value: value, null: value == nil})
		return nil
	})
}

// WhereIn adds an AND IN comparison.
func WhereIn(column string, values ...any) Option {
	return optionFunc(func(query *queryOptions) error {
		if len(values) == 0 {
			return errors.New("orm: WhereIn requires at least one value")
		}
		query.conditions = append(query.conditions, condition{column: column, values: values, in: true})
		return nil
	})
}

// WhereNotIn adds an AND NOT IN comparison.
func WhereNotIn(column string, values ...any) Option {
	return optionFunc(func(query *queryOptions) error {
		if len(values) == 0 {
			return errors.New("orm: WhereNotIn requires at least one value")
		}
		query.conditions = append(query.conditions, condition{column: column, values: values, in: true, not: true})
		return nil
	})
}

// OrderBy adds an ordering using a model field or column name.
func OrderBy(column string, direction Direction) Option {
	return optionFunc(func(query *queryOptions) error {
		if direction != Ascending && direction != Descending {
			return fmt.Errorf("orm: invalid order direction %q", direction)
		}
		query.order = append(query.order, ordering{column: column, direction: direction})
		return nil
	})
}

// Limit restricts the maximum number of returned rows.
func Limit(limit int) Option {
	return optionFunc(func(query *queryOptions) error {
		if limit < 0 {
			return errors.New("orm: limit must not be negative")
		}
		query.limit, query.limitSet = limit, true
		return nil
	})
}

// Offset skips rows before returning results.
func Offset(offset int) Option {
	return optionFunc(func(query *queryOptions) error {
		if offset < 0 {
			return errors.New("orm: offset must not be negative")
		}
		query.offset, query.offsetSet = offset, true
		return nil
	})
}

// Find returns models matching all supplied options.
func Find[T any](ctx context.Context, db *DB, options ...Option) ([]T, error) {
	if err := db.validate(); err != nil {
		return nil, err
	}
	typeOf, err := genericModelType[T]()
	if err != nil {
		return nil, err
	}
	info, err := modelForType(typeOf)
	if err != nil {
		return nil, err
	}
	query, arguments, err := selectSQL(db.dialect, info, options)
	if err != nil {
		return nil, err
	}
	rows, err := db.runner.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("orm: find %s: %w", info.table, err)
	}
	defer rows.Close()

	result := make([]T, 0)
	for rows.Next() {
		value := reflect.New(typeOf)
		if err := scanModel(rows, value.Elem(), info); err != nil {
			return nil, fmt.Errorf("orm: scan %s: %w", info.table, err)
		}
		result = append(result, value.Elem().Interface().(T))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orm: find %s: %w", info.table, err)
	}
	return result, nil
}

// First returns the first model matching all supplied options. It returns
// database/sql.ErrNoRows when no row matches.
func First[T any](ctx context.Context, db *DB, options ...Option) (T, error) {
	var zero T
	if err := db.validate(); err != nil {
		return zero, err
	}
	typeOf, err := genericModelType[T]()
	if err != nil {
		return zero, err
	}
	info, err := modelForType(typeOf)
	if err != nil {
		return zero, err
	}
	options = append(options, Limit(1))
	query, arguments, err := selectSQL(db.dialect, info, options)
	if err != nil {
		return zero, err
	}
	value := reflect.New(typeOf)
	if err := scanModel(db.runner.QueryRowContext(ctx, query, arguments...), value.Elem(), info); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, sql.ErrNoRows
		}
		return zero, fmt.Errorf("orm: find first %s: %w", info.table, err)
	}
	return value.Elem().Interface().(T), nil
}

// Get returns a model by its primary key. Composite keys are supplied in model
// field order. It returns database/sql.ErrNoRows when no row matches.
func Get[T any](ctx context.Context, db *DB, keys ...any) (T, error) {
	var zero T
	typeOf, err := genericModelType[T]()
	if err != nil {
		return zero, err
	}
	info, err := modelForType(typeOf)
	if err != nil {
		return zero, err
	}
	if len(info.primary) == 0 {
		return zero, fmt.Errorf("orm: get %s requires a primary key", info.table)
	}
	if len(keys) != len(info.primary) {
		return zero, fmt.Errorf("orm: get %s requires %d primary-key values, got %d", info.table, len(info.primary), len(keys))
	}
	options := make([]Option, len(keys))
	for i, key := range keys {
		options[i] = Where(info.fields[info.primary[i]].column, Equal, key)
	}
	return First[T](ctx, db, options...)
}

type scanner interface {
	Scan(...any) error
}

func scanModel(row scanner, value reflect.Value, info *modelInfo) error {
	destinations := make([]any, len(info.fields))
	for i, field := range info.fields {
		fieldValue, err := fieldValue(value, field, true)
		if err != nil {
			return err
		}
		destinations[i] = fieldValue.Addr().Interface()
	}
	return row.Scan(destinations...)
}

func selectSQL(dialect Dialect, info *modelInfo, options []Option) (string, []any, error) {
	settings := queryOptions{}
	for _, option := range options {
		if option == nil {
			return "", nil, errors.New("orm: query option must not be nil")
		}
		if err := option.apply(&settings); err != nil {
			return "", nil, err
		}
	}

	columns := make([]string, len(info.fields))
	for i, field := range info.fields {
		columns[i], _ = quoteIdentifier(field.column)
	}
	table, _ := quoteIdentifier(info.table)
	query := "SELECT " + strings.Join(columns, ", ") + " FROM " + table
	arguments := make([]any, 0, len(settings.conditions))
	conditions := make([]string, len(settings.conditions))
	for i, condition := range settings.conditions {
		column, err := modelColumn(info, condition.column)
		if err != nil {
			return "", nil, err
		}
		if condition.in {
			placeholders := make([]string, len(condition.values))
			for index, value := range condition.values {
				arguments = append(arguments, value)
				placeholders[index] = dialect.placeholder(len(arguments))
			}
			operator := " IN "
			if condition.not {
				operator = " NOT IN "
			}
			conditions[i] = column + operator + "(" + strings.Join(placeholders, ", ") + ")"
			continue
		}
		if condition.null {
			if condition.operator != Equal && condition.operator != NotEqual {
				return "", nil, fmt.Errorf("orm: nil can only be compared with Equal or NotEqual")
			}
			if condition.operator == Equal {
				conditions[i] = column + " IS NULL"
			} else {
				conditions[i] = column + " IS NOT NULL"
			}
			continue
		}
		arguments = append(arguments, condition.value)
		conditions[i] = column + " " + string(condition.operator) + " " + dialect.placeholder(len(arguments))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	if len(settings.order) > 0 {
		order := make([]string, len(settings.order))
		for i, ordering := range settings.order {
			column, err := modelColumn(info, ordering.column)
			if err != nil {
				return "", nil, err
			}
			order[i] = column + " " + string(ordering.direction)
		}
		query += " ORDER BY " + strings.Join(order, ", ")
	}
	if settings.limitSet {
		query += " LIMIT " + fmt.Sprint(settings.limit)
	}
	if settings.offsetSet {
		if !settings.limitSet && dialect == SQLite {
			query += " LIMIT -1"
		}
		query += " OFFSET " + fmt.Sprint(settings.offset)
	}
	return query, arguments, nil
}

func genericModelType[T any]() (reflect.Type, error) {
	typeOf := reflect.TypeFor[T]()
	if typeOf.Kind() != reflect.Struct {
		return nil, fmt.Errorf("orm: result type must be a struct, got %s", typeOf)
	}
	return typeOf, nil
}

func modelColumn(info *modelInfo, name string) (string, error) {
	index, exists := info.byName[name]
	if !exists {
		return "", fmt.Errorf("orm: model %s has no field or column %q", info.typ, name)
	}
	return quoteIdentifier(info.fields[index].column)
}

func validOperator(operator Operator) bool {
	switch operator {
	case Equal, NotEqual, LessThan, LessThanOrEqual, GreaterThan, GreaterThanOrEqual, Like:
		return true
	default:
		return false
	}
}
