package orm

import (
	"strings"
	"testing"
)

func TestCreateTableSQL(t *testing.T) {
	info, err := modelFor(testUser{})
	if err != nil {
		t.Fatal(err)
	}

	sqlite, err := createTableSQL(SQLite, info)
	if err != nil {
		t.Fatal(err)
	}
	wantSQLite := `CREATE TABLE IF NOT EXISTS "test_users" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "account_id" INTEGER NOT NULL, "email" TEXT UNIQUE, "data" BLOB, "created_at" DATETIME, "nickname" TEXT)`
	if sqlite != wantSQLite {
		t.Fatalf("SQLite migration:\n%s\nwant:\n%s", sqlite, wantSQLite)
	}

	postgres, err := createTableSQL(PostgreSQL, info)
	if err != nil {
		t.Fatal(err)
	}
	wantPostgres := `CREATE TABLE IF NOT EXISTS "test_users" ("id" BIGSERIAL PRIMARY KEY, "account_id" BIGINT NOT NULL, "email" TEXT UNIQUE, "data" BYTEA, "created_at" TIMESTAMPTZ, "nickname" TEXT)`
	if postgres != wantPostgres {
		t.Fatalf("PostgreSQL migration:\n%s\nwant:\n%s", postgres, wantPostgres)
	}
}

func TestCompositePrimaryKey(t *testing.T) {
	type membership struct {
		UserID int64 `orm:"primary"`
		TeamID int64 `orm:"primary"`
	}
	info, err := modelFor(membership{})
	if err != nil {
		t.Fatal(err)
	}
	statement, err := createTableSQL(PostgreSQL, info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(statement, `PRIMARY KEY ("user_id", "team_id"))`) {
		t.Fatalf("migration does not contain a composite primary key: %s", statement)
	}
}
