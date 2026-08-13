# ORM

Package `github.com/nathanpls/godo/orm` maps Go structs to SQLite and PostgreSQL through the standard
`database/sql` package. It provides schema creation, CRUD, safe filters,
transactions, and raw SQL access.

The package does not select or import a SQL driver. Applications remain free to
choose the driver that fits their deployment.

## Connect

### SQLite

Install a SQLite driver, such as the pure-Go `modernc.org/sqlite` driver:

```sh
go get modernc.org/sqlite
```

```go
import (
    "database/sql"

    "github.com/nathanpls/godo/orm"
    _ "modernc.org/sqlite"
)

sqlDB, err := sql.Open("sqlite", "app.db")
if err != nil {
    return err
}
defer sqlDB.Close()

db := orm.New(sqlDB, orm.SQLite)
```

### PostgreSQL

Install a PostgreSQL `database/sql` driver, such as pgx:

```sh
go get github.com/jackc/pgx/v5/stdlib
```

```go
import (
    "database/sql"

    "github.com/nathanpls/godo/orm"
    _ "github.com/jackc/pgx/v5/stdlib"
)

sqlDB, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
if err != nil {
    return err
}
defer sqlDB.Close()

db := orm.New(sqlDB, orm.PostgreSQL)
```

`orm.Open` is a shorter alternative after importing a driver:

```go
db, err := orm.Open("pgx", os.Getenv("DATABASE_URL"), orm.PostgreSQL)
if err != nil {
    return err
}
defer db.SQLDB().Close()
```

`sql.Open` and `orm.Open` do not verify the connection immediately. Use
`db.SQLDB().PingContext(ctx)` during application startup when connectivity must
be checked.

## Models

Exported struct fields become columns. Names use snake_case and table names use
plural snake_case:

```go
type User struct {
    ID        int64
    AccountID int64
    Email     string
    CreatedAt time.Time
}
```

This maps to table `users` with columns `id`, `account_id`, `email`, and
`created_at`. An integer field named `ID` is an auto-generated primary key by
convention.

Use `orm` tags for explicit behavior:

```go
type User struct {
    Key      int64  `orm:"user_id,primary,auto"`
    Email    string `orm:"unique,notnull"`
    Nickname *string
    Cached   string `orm:"-"`
}
```

Supported tag values:

| Value | Meaning |
|---|---|
| First value | Override the column name |
| `primary` or `pk` | Primary-key field |
| `auto` | Database-generated integer primary key |
| `unique` | Add a unique constraint |
| `notnull` | Add a not-null constraint |
| `-` | Ignore the field |

Override a table name with `TableName`:

```go
func (User) TableName() string { return "accounts" }
```

Embedded structs are flattened. Supported field types include booleans,
integers, unsigned integers, floats, strings, byte slices, `time.Time`, pointers
to supported types, and the standard `sql.Null*` types.

## Migrations

For direct development-time table creation, create missing tables:

```go
if err := db.Migrate(ctx, User{}, Session{}); err != nil {
    return err
}
```

`Migrate` uses `CREATE TABLE IF NOT EXISTS`. In this version it deliberately
does not add, remove, or modify columns on existing tables. Manage schema changes
with the godo migration workflow below.

### Initialize a project

From a Go module, initialize SQLite or PostgreSQL:

```sh
godo db init --dialect sqlite
# or
godo db init --dialect postgres
```

This creates:

```text
db/
├── godo/
│   └── main.go
├── migrations/
└── schema.lock.json
```

It also prints the driver command needed by the generated program. SQLite uses
`modernc.org/sqlite`; PostgreSQL uses `github.com/jackc/pgx/v5/stdlib`.

### Register models

Edit `db/godo/main.go` and import application model packages:

```go
package main

import (
    "github.com/nathanpls/godo/orm"
    "github.com/nathanpls/godo/orm/dbtool"

    _ "modernc.org/sqlite"

    "example.com/app/models"
)

func main() {
    dbtool.Main(dbtool.Config{
        Dialect: orm.SQLite,
        Driver:  "sqlite",
        Source:  dbtool.Env("DATABASE_URL", "db/app.db"),
        Models: []any{
            models.User{},
            models.Session{},
        },
    })
}
```

The schema program is ordinary compiler-checked Go. The application owns its SQL
driver; the `godo` CLI does not bundle SQLite or PostgreSQL drivers.

### Generate migrations

Compile the registered models, compare them with `schema.lock.json`, and produce
reviewable up/down SQL:

```sh
godo db generate create_users
```

Generated files use monotonically increasing versions:

```text
db/migrations/
├── 000001_create_users.up.sql
└── 000001_create_users.down.sql
```

Automatic generation supports new tables and nullable unconstrained columns. It
rejects destructive or ambiguous changes such as dropped tables, dropped
columns, changed types, changed primary keys, or new constrained columns. Write
those changes explicitly instead of relying on a guess.

Create an empty up/down pair for custom or data SQL when models have not changed:

```sh
godo db generate normalize_emails --empty
```

Empty files cannot be applied until SQL is written into both directions. A
project lock prevents concurrent generators from selecting the same version.

### Apply migrations

```sh
godo db migrate
```

Migrations run in version order inside a database transaction. The database
records each version, name, checksum, and application timestamp in
`_godo_migrations`. Applied files cannot be modified or removed without causing
a validation error.

Migration operations use a PostgreSQL advisory lock or a SQLite lock row so two
processes cannot migrate or roll back concurrently.

### Status

```sh
godo db status
```

Status reports pending, applied, modified, and missing migrations.

### Roll back

Roll back the latest applied migration:

```sh
godo db rollback
```

The down SQL and migration-record removal run in one transaction.

### Database source

The generated schema program reads `DATABASE_URL`. SQLite defaults to
`db/app.db`; PostgreSQL intentionally has no default:

```sh
DATABASE_URL='postgres://localhost/app?sslmode=disable' godo db migrate
```

Database commands locate the module root through `go.mod`, so they can be run
from a child directory.

## Create

Insert requires a pointer so an automatic ID can be written back:

```go
user := User{Email: "hello@example.com"}
if err := db.Insert(ctx, &user); err != nil {
    return err
}

fmt.Println(user.ID)
```

## Read

Retrieve a model by primary key:

```go
user, err := orm.Get[User](ctx, db, 42)
if errors.Is(err, sql.ErrNoRows) {
    // Not found.
}
```

Return the first matching model:

```go
user, err := orm.First[User](ctx, db,
    orm.Where("Email", orm.Equal, "hello@example.com"),
)
```

Return all matching models:

```go
users, err := orm.Find[User](ctx, db,
    orm.Where("AccountID", orm.Equal, accountID),
    orm.WhereIn("id", int64(1), int64(2), int64(3)),
    orm.OrderBy("CreatedAt", orm.Descending),
    orm.Limit(20),
    orm.Offset(40),
)
```

Filters accept a Go field name or mapped column name. Unknown names are rejected
rather than inserted into SQL. Available operators are:

```go
orm.Equal
orm.NotEqual
orm.LessThan
orm.LessThanOrEqual
orm.GreaterThan
orm.GreaterThanOrEqual
orm.Like
```

Comparing `nil` with `Equal` or `NotEqual` generates `IS NULL` or `IS NOT NULL`.
`WhereIn` and `WhereNotIn` require at least one value.

## Update

`Update` writes every non-primary field and uses the primary key in the `WHERE`
clause:

```go
user.Email = "new@example.com"
if err := db.Update(ctx, &user); err != nil {
    return err
}
```

A zero primary key is rejected to prevent an accidental broad update.

## Delete

```go
if err := db.Delete(ctx, &user); err != nil {
    return err
}
```

Delete also requires non-zero primary-key values.

## Transactions

```go
err := db.Transaction(ctx, nil, func(tx *orm.DB) error {
    if err := tx.Insert(ctx, &user); err != nil {
        return err
    }
    return tx.Insert(ctx, &session)
})
```

Returning an error rolls back. A nil error commits. Nested transactions are not
supported.

## Raw SQL

Use the current pool or transaction directly when a query does not fit the model
API:

```go
result, err := db.Exec(ctx, "DELETE FROM sessions WHERE expires_at < $1", now)
rows, err := db.Query(ctx, "SELECT id FROM users WHERE active = $1", true)
err := db.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count)
```

Raw SQL is passed through unchanged. Use the placeholder style expected by the
selected database.
