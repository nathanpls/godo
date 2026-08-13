// Package orm maps Go structs to SQLite and PostgreSQL using database/sql.
//
// Applications choose and import their own database driver. The ORM does not
// force a driver dependency:
//
//	sqlDB, err := sql.Open("sqlite", "app.db")
//	if err != nil {
//		log.Fatal(err)
//	}
//	db := orm.New(sqlDB, orm.SQLite)
//
//	type User struct {
//		ID    int64
//		Email string `orm:"unique,notnull"`
//		Name  string
//	}
//
//	if err := db.Migrate(ctx, User{}); err != nil {
//		log.Fatal(err)
//	}
//
//	user := User{Email: "hello@example.com", Name: "Gopher"}
//	if err := db.Insert(ctx, &user); err != nil {
//		log.Fatal(err)
//	}
//
//	users, err := orm.Find[User](ctx, db,
//		orm.Where("email", orm.Equal, "hello@example.com"),
//	)
package orm
