package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nathanpls/godo/core/orm"
)

// Bucket is the model used by ORMStore. Register it in db/godo/main.go when
// schema migrations are managed with godo db.
type Bucket struct {
	Key     string `orm:"primary"`
	Count   int64  `orm:"notnull"`
	ResetAt int64  `orm:"notnull"`
}

// TableName returns the shared rate-limit table name.
func (Bucket) TableName() string { return "godo_rate_limits" }

// ORMStore coordinates limits through SQLite or PostgreSQL.
type ORMStore struct {
	db          *orm.DB
	cleanupMu   sync.Mutex
	nextCleanup time.Time
}

// NewORMStore creates a shared Store. Its table must exist before requests are
// handled; register Bucket in the schema program or call Migrate during local
// development.
func NewORMStore(db *orm.DB) *ORMStore {
	return &ORMStore{db: db}
}

// Migrate creates the rate-limit table if it is missing. Prefer registered,
// reviewable migrations for production schema changes.
func (store *ORMStore) Migrate(ctx context.Context) error {
	if store == nil || store.db == nil {
		return errors.New("ratelimit: ORM database must not be nil")
	}
	return store.db.Migrate(ctx, Bucket{})
}

// Take atomically consumes one request from key's current window.
func (store *ORMStore) Take(ctx context.Context, key string, limit int64, window time.Duration, now time.Time) (Result, error) {
	if store == nil || store.db == nil {
		return Result{}, errors.New("ratelimit: ORM database must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := store.cleanup(ctx, now, window); err != nil {
		return Result{}, err
	}
	query, err := ormTakeSQL(store.db.Dialect())
	if err != nil {
		return Result{}, err
	}
	var count int64
	var storedReset int64
	windowSeconds := int64(window / time.Second)
	if window%time.Second != 0 {
		windowSeconds++
	}
	if err := store.db.QueryRow(ctx, query, key, windowSeconds, limit).Scan(&count, &storedReset); err != nil {
		return Result{}, fmt.Errorf("ratelimit: consume ORM limit: %w", err)
	}
	return Result{
		Allowed:   count <= limit,
		Remaining: max(0, limit-count),
		Reset:     time.Unix(storedReset, 0),
	}, nil
}

func (store *ORMStore) cleanup(ctx context.Context, now time.Time, window time.Duration) error {
	store.cleanupMu.Lock()
	defer store.cleanupMu.Unlock()
	if now.Before(store.nextCleanup) {
		return nil
	}
	query := `DELETE FROM "godo_rate_limits" WHERE "reset_at" <= unixepoch()`
	if store.db.Dialect() == orm.PostgreSQL {
		query = `DELETE FROM "godo_rate_limits" WHERE "reset_at" <= EXTRACT(EPOCH FROM clock_timestamp())::BIGINT`
	}
	if _, err := store.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("ratelimit: clean expired ORM limits: %w", err)
	}
	interval := min(window, time.Minute)
	store.nextCleanup = now.Add(max(interval, time.Second))
	return nil
}

func ormTakeSQL(dialect orm.Dialect) (string, error) {
	switch dialect {
	case orm.SQLite:
		return `INSERT INTO "godo_rate_limits" ("key", "count", "reset_at") VALUES (?, 1, unixepoch() + ?)
ON CONFLICT ("key") DO UPDATE SET
  "count" = CASE WHEN "reset_at" <= unixepoch() THEN 1 WHEN "count" <= ? THEN "count" + 1 ELSE "count" END,
  "reset_at" = CASE WHEN "reset_at" <= unixepoch() THEN excluded."reset_at" ELSE "reset_at" END
RETURNING "count", "reset_at"`, nil
	case orm.PostgreSQL:
		return `INSERT INTO "godo_rate_limits" ("key", "count", "reset_at") VALUES ($1, 1, EXTRACT(EPOCH FROM statement_timestamp())::BIGINT + $2)
ON CONFLICT ("key") DO UPDATE SET
	  "count" = CASE WHEN "godo_rate_limits"."reset_at" <= EXTRACT(EPOCH FROM statement_timestamp())::BIGINT THEN 1 WHEN "godo_rate_limits"."count" <= $3 THEN "godo_rate_limits"."count" + 1 ELSE "godo_rate_limits"."count" END,
	  "reset_at" = CASE WHEN "godo_rate_limits"."reset_at" <= EXTRACT(EPOCH FROM statement_timestamp())::BIGINT THEN excluded."reset_at" ELSE "godo_rate_limits"."reset_at" END
RETURNING "count", "reset_at"`, nil
	default:
		return "", fmt.Errorf("ratelimit: unsupported ORM dialect %d", dialect)
	}
}
