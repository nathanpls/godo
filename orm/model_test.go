package orm

import (
	"database/sql"
	"reflect"
	"testing"
	"time"
)

type testUser struct {
	ID        int64
	AccountID int64  `orm:"account_id,notnull"`
	Email     string `orm:"unique"`
	Data      []byte
	CreatedAt time.Time
	Nickname  *string
	Ignored   string `orm:"-"`
}

func TestModelConventions(t *testing.T) {
	info, err := modelFor(testUser{})
	if err != nil {
		t.Fatal(err)
	}
	if info.table != "test_users" {
		t.Fatalf("table = %q, want test_users", info.table)
	}
	if info.autoField != 0 || !info.fields[0].primary || !info.fields[0].auto {
		t.Fatalf("ID field is not an automatic primary key: %+v", info.fields[0])
	}
	if len(info.fields) != 6 {
		t.Fatalf("field count = %d, want 6", len(info.fields))
	}
	if info.fields[1].column != "account_id" || !info.fields[1].notNull {
		t.Fatalf("AccountID metadata = %+v", info.fields[1])
	}
}

func TestSnakeCaseAndPluralize(t *testing.T) {
	tests := map[string]string{
		"User":        "users",
		"APIKey":      "api_keys",
		"Category":    "categories",
		"PostalBox":   "postal_boxes",
		"HTTPAddress": "http_addresses",
	}
	for input, want := range tests {
		if got := pluralize(snakeCase(input)); got != want {
			t.Errorf("%s => %s, want %s", input, got, want)
		}
	}
}

func TestKnownSchemaTypes(t *testing.T) {
	tests := []struct {
		typeOf reflect.Type
		sqlite string
		pg     string
	}{
		{reflect.TypeFor[time.Time](), "DATETIME", "TIMESTAMPTZ"},
		{reflect.TypeFor[sql.NullString](), "TEXT", "TEXT"},
		{reflect.TypeFor[sql.NullInt64](), "INTEGER", "BIGINT"},
	}
	for _, test := range tests {
		field := modelField{goName: "Value", typ: test.typeOf}
		if got, err := SQLite.columnType(field); err != nil || got != test.sqlite {
			t.Errorf("SQLite %s = %q, %v; want %q", test.typeOf, got, err, test.sqlite)
		}
		if got, err := PostgreSQL.columnType(field); err != nil || got != test.pg {
			t.Errorf("PostgreSQL %s = %q, %v; want %q", test.typeOf, got, err, test.pg)
		}
	}
}

func TestRejectsInvalidModelMetadata(t *testing.T) {
	type invalid struct {
		ID   int64
		Name string `orm:"name; DROP TABLE users"`
	}
	if _, err := modelFor(invalid{}); err == nil {
		t.Fatal("invalid column name was accepted")
	}

	type invalidAuto struct {
		Key string `orm:"primary,auto"`
	}
	if _, err := modelFor(invalidAuto{}); err == nil {
		t.Fatal("non-integer auto field was accepted")
	}

	type narrowAuto struct {
		Key int32 `orm:"primary,auto"`
	}
	if _, err := modelFor(narrowAuto{}); err == nil {
		t.Fatal("narrow auto field was accepted")
	}
}

func TestRejectsCyclicEmbeddedModel(t *testing.T) {
	type Node struct {
		ID int64
		*Node
	}
	if _, err := modelFor(Node{}); err == nil {
		t.Fatal("cyclic embedded model was accepted")
	}
}

func TestRejectsAmbiguousAliases(t *testing.T) {
	type ambiguous struct {
		First  string `orm:"Second"`
		Second string `orm:"other"`
	}
	if _, err := modelFor(ambiguous{}); err == nil {
		t.Fatal("ambiguous field and column aliases were accepted")
	}
}
