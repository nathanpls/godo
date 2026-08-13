package ratelimit

import (
	"strings"
	"testing"
	"time"

	"github.com/nathanpls/godo/orm"
)

func TestORMTakeSQL(t *testing.T) {
	sqlite, err := ormTakeSQL(orm.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sqlite, `VALUES (?, 1, unixepoch() + ?)`) || !strings.Contains(sqlite, `ON CONFLICT ("key") DO UPDATE`) || !strings.Contains(sqlite, `RETURNING "count", "reset_at"`) {
		t.Fatalf("SQLite query:\n%s", sqlite)
	}

	postgres, err := ormTakeSQL(orm.PostgreSQL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(postgres, `EXTRACT(EPOCH FROM statement_timestamp())::BIGINT + $2`) || !strings.Contains(postgres, `"godo_rate_limits"."count" <= $3`) || !strings.Contains(postgres, `RETURNING "count", "reset_at"`) {
		t.Fatalf("PostgreSQL query:\n%s", postgres)
	}
}

func TestBucketSchema(t *testing.T) {
	for _, dialect := range []orm.Dialect{orm.SQLite, orm.PostgreSQL} {
		schema, err := orm.Describe(dialect, Bucket{})
		if err != nil {
			t.Fatal(err)
		}
		if len(schema.Tables) != 1 || schema.Tables[0].Name != "godo_rate_limits" {
			t.Fatalf("schema = %+v", schema)
		}
		if len(schema.Tables[0].PrimaryKey) != 1 || schema.Tables[0].PrimaryKey[0] != "key" {
			t.Fatalf("primary key = %+v", schema.Tables[0].PrimaryKey)
		}
	}
}

func TestNilORMStore(t *testing.T) {
	var store *ORMStore
	if err := store.Migrate(t.Context()); err == nil {
		t.Fatal("nil ORM store migration succeeded")
	}
	if _, err := store.Take(t.Context(), "key", 1, time.Second, time.Time{}); err == nil {
		t.Fatal("nil ORM store take succeeded")
	}
}
