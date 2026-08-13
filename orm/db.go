package orm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Row is the result of a query expected to return at most one row.
type Row interface {
	Scan(...any) error
}

type errorRow struct{ err error }

func (row errorRow) Scan(...any) error { return row.err }

// DB maps models to a database/sql connection pool or transaction.
type DB struct {
	sql     *sql.DB
	runner  sqlRunner
	dialect Dialect
	inTx    bool
}

// New creates an ORM using an application-owned database connection pool.
func New(database *sql.DB, dialect Dialect) *DB {
	return &DB{sql: database, runner: database, dialect: dialect}
}

// Open creates a database/sql pool and ORM. The requested driver must be
// imported by the application. The caller owns the returned pool and should
// close db.SQLDB when it is no longer needed.
func Open(driverName, dataSourceName string, dialect Dialect) (*DB, error) {
	if err := dialect.validate(); err != nil {
		return nil, err
	}
	database, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("orm: open database: %w", err)
	}
	return New(database, dialect), nil
}

// Dialect returns the configured SQL dialect.
func (db *DB) Dialect() Dialect {
	return db.dialect
}

// SQLDB returns the application-owned database connection pool.
func (db *DB) SQLDB() *sql.DB {
	return db.sql
}

// Exec executes raw SQL through the current connection pool or transaction.
func (db *DB) Exec(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	if err := db.validate(); err != nil {
		return nil, err
	}
	return db.runner.ExecContext(ctx, query, arguments...)
}

// Query executes a raw query through the current connection pool or
// transaction. The caller must close the returned rows.
func (db *DB) Query(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	if err := db.validate(); err != nil {
		return nil, err
	}
	return db.runner.QueryContext(ctx, query, arguments...)
}

// QueryRow executes a raw query expected to return at most one row. Errors are
// returned by Row.Scan.
func (db *DB) QueryRow(ctx context.Context, query string, arguments ...any) Row {
	if err := db.validate(); err != nil {
		return errorRow{err: err}
	}
	return db.runner.QueryRowContext(ctx, query, arguments...)
}

// Transaction executes callback in a database transaction. Returning an error
// rolls the transaction back; otherwise it is committed.
func (db *DB) Transaction(ctx context.Context, options *sql.TxOptions, callback func(*DB) error) error {
	if err := db.validate(); err != nil {
		return err
	}
	if callback == nil {
		return errors.New("orm: transaction callback must not be nil")
	}
	if db.inTx {
		return errors.New("orm: nested transactions are not supported")
	}

	tx, err := db.sql.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("orm: begin transaction: %w", err)
	}
	defer tx.Rollback()
	txDB := &DB{sql: db.sql, runner: tx, dialect: db.dialect, inTx: true}
	if err := callback(txDB); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("orm: rollback transaction: %w", rollbackErr))
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("orm: commit transaction: %w", err)
	}
	return nil
}

func (db *DB) validate() error {
	if db == nil || db.sql == nil || db.runner == nil {
		return errors.New("orm: database must not be nil")
	}
	return db.dialect.validate()
}
