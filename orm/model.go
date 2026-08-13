package orm

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

// TableNamer overrides the default plural snake_case table name for a model.
type TableNamer interface {
	TableName() string
}

type modelField struct {
	goName  string
	column  string
	index   []int
	typ     reflect.Type
	primary bool
	auto    bool
	unique  bool
	notNull bool
}

type modelInfo struct {
	typ       reflect.Type
	table     string
	fields    []modelField
	primary   []int
	autoField int
	byName    map[string]int
}

var modelCache sync.Map

func modelFor(value any) (*modelInfo, error) {
	if value == nil {
		return nil, errors.New("orm: model must not be nil")
	}
	typeOf := reflect.TypeOf(value)
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	return modelForType(typeOf)
}

func modelForType(typeOf reflect.Type) (*modelInfo, error) {
	if typeOf.Kind() != reflect.Struct {
		return nil, fmt.Errorf("orm: model must be a struct, got %s", typeOf)
	}
	if cached, ok := modelCache.Load(typeOf); ok {
		return cached.(*modelInfo), nil
	}

	table := pluralize(snakeCase(typeOf.Name()))
	pointer := reflect.New(typeOf).Interface()
	if namer, ok := pointer.(TableNamer); ok {
		table = namer.TableName()
	} else if namer, ok := reflect.Zero(typeOf).Interface().(TableNamer); ok {
		table = namer.TableName()
	}
	if _, err := quoteIdentifier(table); err != nil {
		return nil, fmt.Errorf("orm: table name for %s: %w", typeOf, err)
	}

	info := &modelInfo{typ: typeOf, table: table, autoField: -1, byName: make(map[string]int)}
	if err := collectFields(info, typeOf, nil); err != nil {
		return nil, err
	}
	if len(info.fields) == 0 {
		return nil, fmt.Errorf("orm: model %s has no mapped fields", typeOf)
	}
	for i, field := range info.fields {
		if _, exists := info.byName[field.column]; exists {
			return nil, fmt.Errorf("orm: model %s maps multiple fields to column %q", typeOf, field.column)
		}
		info.byName[field.column] = i
		info.byName[field.goName] = i
		if field.primary {
			info.primary = append(info.primary, i)
		}
		if field.auto {
			if info.autoField >= 0 {
				return nil, fmt.Errorf("orm: model %s has multiple auto fields", typeOf)
			}
			if !field.primary {
				return nil, fmt.Errorf("orm: auto field %s must also be primary", field.goName)
			}
			if !isInteger(indirectType(field.typ).Kind()) {
				return nil, fmt.Errorf("orm: auto field %s must be an integer", field.goName)
			}
			info.autoField = i
		}
	}
	if info.autoField >= 0 && len(info.primary) > 1 {
		return nil, fmt.Errorf("orm: model %s cannot use auto with a composite primary key", typeOf)
	}

	actual, _ := modelCache.LoadOrStore(typeOf, info)
	return actual.(*modelInfo), nil
}

func collectFields(info *modelInfo, typeOf reflect.Type, prefix []int) error {
	for i := 0; i < typeOf.NumField(); i++ {
		structField := typeOf.Field(i)
		if !structField.IsExported() {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		fieldType := structField.Type
		embeddedType := fieldType
		if embeddedType.Kind() == reflect.Pointer {
			embeddedType = embeddedType.Elem()
		}
		if structField.Anonymous && embeddedType.Kind() == reflect.Struct && structField.Tag.Get("orm") == "" {
			if err := collectFields(info, embeddedType, index); err != nil {
				return err
			}
			continue
		}

		field, ignored, err := parseField(structField, index)
		if err != nil {
			return fmt.Errorf("orm: model %s field %s: %w", info.typ, structField.Name, err)
		}
		if ignored {
			continue
		}
		if field.column == "id" && structField.Name == "ID" && structField.Tag.Get("orm") == "" {
			field.primary = true
			field.auto = isInteger(indirectType(fieldType).Kind())
		}
		info.fields = append(info.fields, field)
	}
	return nil
}

func parseField(structField reflect.StructField, index []int) (modelField, bool, error) {
	field := modelField{goName: structField.Name, column: snakeCase(structField.Name), index: index, typ: structField.Type}
	tag := structField.Tag.Get("orm")
	if tag == "-" {
		return modelField{}, true, nil
	}
	if tag == "" {
		return field, false, nil
	}
	for position, raw := range strings.Split(tag, ",") {
		option := strings.TrimSpace(raw)
		switch option {
		case "":
		case "primary", "pk":
			field.primary = true
		case "auto":
			field.auto = true
		case "unique":
			field.unique = true
		case "notnull":
			field.notNull = true
		default:
			if position != 0 {
				return modelField{}, false, fmt.Errorf("unknown tag option %q", option)
			}
			if _, err := quoteIdentifier(option); err != nil || strings.Contains(option, ".") {
				return modelField{}, false, fmt.Errorf("invalid column name %q", option)
			}
			field.column = option
		}
	}
	return field, false, nil
}

func indirectType(value reflect.Type) reflect.Type {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value
}

func snakeCase(value string) string {
	runes := []rune(value)
	var result strings.Builder
	for i, current := range runes {
		if unicode.IsUpper(current) {
			if i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) || i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
				result.WriteByte('_')
			}
			result.WriteRune(unicode.ToLower(current))
			continue
		}
		result.WriteRune(current)
	}
	return result.String()
}

func pluralize(value string) string {
	if strings.HasSuffix(value, "ch") || strings.HasSuffix(value, "sh") || strings.HasSuffix(value, "s") || strings.HasSuffix(value, "x") || strings.HasSuffix(value, "z") {
		return value + "es"
	}
	if len(value) > 1 && strings.HasSuffix(value, "y") {
		previous := value[len(value)-2]
		if !strings.ContainsRune("aeiou", rune(previous)) {
			return value[:len(value)-1] + "ies"
		}
	}
	return value + "s"
}

func fieldValue(root reflect.Value, field modelField, allocate bool) (reflect.Value, error) {
	current := root
	for current.Kind() == reflect.Pointer {
		if current.IsNil() {
			if !allocate || !current.CanSet() {
				return reflect.Value{}, fmt.Errorf("orm: embedded pointer for field %s is nil", field.goName)
			}
			current.Set(reflect.New(current.Type().Elem()))
		}
		current = current.Elem()
	}
	for position, index := range field.index {
		current = current.Field(index)
		if position == len(field.index)-1 {
			return current, nil
		}
		if current.Kind() == reflect.Pointer {
			if current.IsNil() {
				if !allocate || !current.CanSet() {
					return reflect.Value{}, fmt.Errorf("orm: embedded pointer for field %s is nil", field.goName)
				}
				current.Set(reflect.New(current.Type().Elem()))
			}
			current = current.Elem()
		}
	}
	return current, nil
}
