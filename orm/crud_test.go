package orm

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

type recordingRunner struct {
	query     string
	arguments []any
	result    sql.Result
	err       error
}

func (runner *recordingRunner) ExecContext(_ context.Context, query string, arguments ...any) (sql.Result, error) {
	runner.query = query
	runner.arguments = arguments
	return runner.result, runner.err
}

func (*recordingRunner) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	panic("unexpected query")
}

func (*recordingRunner) QueryRowContext(context.Context, string, ...any) *sql.Row {
	panic("unexpected query row")
}

type testResult struct {
	id   int64
	rows int64
}

func (result testResult) LastInsertId() (int64, error) { return result.id, nil }
func (result testResult) RowsAffected() (int64, error) { return result.rows, nil }

func TestSQLiteCRUD(t *testing.T) {
	runner := &recordingRunner{result: testResult{id: 42, rows: 1}}
	db := &DB{sql: &sql.DB{}, runner: runner, dialect: SQLite}
	user := testUser{AccountID: 7, Email: "hello@example.com"}

	if err := db.Insert(context.Background(), &user); err != nil {
		t.Fatal(err)
	}
	if user.ID != 42 {
		t.Fatalf("generated ID = %d, want 42", user.ID)
	}
	wantInsert := `INSERT INTO "test_users" ("account_id", "email", "data", "created_at", "nickname") VALUES (?, ?, ?, ?, ?)`
	if runner.query != wantInsert {
		t.Fatalf("insert = %s, want %s", runner.query, wantInsert)
	}

	user.Email = "updated@example.com"
	if err := db.Update(context.Background(), &user); err != nil {
		t.Fatal(err)
	}
	wantUpdate := `UPDATE "test_users" SET "account_id" = ?, "email" = ?, "data" = ?, "created_at" = ?, "nickname" = ? WHERE "id" = ?`
	if runner.query != wantUpdate {
		t.Fatalf("update = %s, want %s", runner.query, wantUpdate)
	}
	if got := runner.arguments[len(runner.arguments)-1]; got != int64(42) {
		t.Fatalf("update primary key = %#v", got)
	}

	if err := db.Delete(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if runner.query != `DELETE FROM "test_users" WHERE "id" = ?` || !reflect.DeepEqual(runner.arguments, []any{int64(42)}) {
		t.Fatalf("delete = %s %#v", runner.query, runner.arguments)
	}
}

func TestUpdateRequiresPrimaryKeyValue(t *testing.T) {
	runner := &recordingRunner{result: testResult{rows: 1}}
	db := &DB{sql: &sql.DB{}, runner: runner, dialect: SQLite}
	if err := db.Update(context.Background(), &testUser{}); err == nil {
		t.Fatal("update with zero primary key succeeded")
	}
}

func TestSetGeneratedUnsignedID(t *testing.T) {
	type record struct{ ID uint64 }
	value := record{}
	root, info, err := mutableModel(&value)
	if err != nil {
		t.Fatal(err)
	}
	if err := setGeneratedID(root, info.fields[0], 12); err != nil {
		t.Fatal(err)
	}
	if value.ID != 12 {
		t.Fatalf("ID = %d, want 12", value.ID)
	}
}
