package orm

import (
	"reflect"
	"testing"
)

func TestPostgreSQLSelect(t *testing.T) {
	info, err := modelFor(testUser{})
	if err != nil {
		t.Fatal(err)
	}
	query, arguments, err := selectSQL(PostgreSQL, info, []Option{
		Where("Email", Equal, "hello@example.com"),
		WhereIn("id", int64(1), int64(2)),
		OrderBy("CreatedAt", Descending),
		Limit(5),
		Offset(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "id", "account_id", "email", "data", "created_at", "nickname" FROM "test_users" WHERE "email" = $1 AND "id" IN ($2, $3) ORDER BY "created_at" DESC LIMIT 5 OFFSET 2`
	if query != want {
		t.Fatalf("query:\n%s\nwant:\n%s", query, want)
	}
	if !reflect.DeepEqual(arguments, []any{"hello@example.com", int64(1), int64(2)}) {
		t.Fatalf("arguments = %#v", arguments)
	}
}

func TestSQLiteNullAndOffsetSelect(t *testing.T) {
	info, err := modelFor(testUser{})
	if err != nil {
		t.Fatal(err)
	}
	query, arguments, err := selectSQL(SQLite, info, []Option{
		Where("Nickname", Equal, nil),
		Offset(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "id", "account_id", "email", "data", "created_at", "nickname" FROM "test_users" WHERE "nickname" IS NULL LIMIT -1 OFFSET 10`
	if query != want || len(arguments) != 0 {
		t.Fatalf("query = %s, arguments = %#v", query, arguments)
	}
}

func TestQueryRejectsUnknownColumn(t *testing.T) {
	info, err := modelFor(testUser{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := selectSQL(SQLite, info, []Option{Where("not_a_column", Equal, 1)}); err == nil {
		t.Fatal("unknown column was accepted")
	}
}

func TestScanModel(t *testing.T) {
	info, err := modelFor(testUser{})
	if err != nil {
		t.Fatal(err)
	}
	var user testUser
	scanner := valueScanner{values: []any{int64(4), int64(7), "hello@example.com", []byte("data"), reflect.Zero(reflect.TypeFor[testUser]()).FieldByName("CreatedAt").Interface(), nil}}
	if err := scanModel(scanner, reflect.ValueOf(&user).Elem(), info); err != nil {
		t.Fatal(err)
	}
	if user.ID != 4 || user.Email != "hello@example.com" || string(user.Data) != "data" || user.Nickname != nil {
		t.Fatalf("scanned user = %+v", user)
	}
}

type valueScanner struct{ values []any }

func (scanner valueScanner) Scan(destinations ...any) error {
	for i, destination := range destinations {
		target := reflect.ValueOf(destination).Elem()
		value := scanner.values[i]
		if value == nil {
			target.SetZero()
			continue
		}
		target.Set(reflect.ValueOf(value))
	}
	return nil
}
