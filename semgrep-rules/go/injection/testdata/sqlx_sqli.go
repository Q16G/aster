package testdata

import "fmt"

type SQLXDB struct{}

func (db *SQLXDB) Queryx(query string, args ...interface{})                {}
func (db *SQLXDB) QueryRowx(query string, args ...interface{})             {}
func (db *SQLXDB) Get(dest interface{}, query string, args ...interface{}) {}
func (db *SQLXDB) Select(dest interface{}, query string, args ...interface{}) {}
func (db *SQLXDB) NamedQuery(query string, arg interface{})                {}
func (db *SQLXDB) NamedExec(query string, arg interface{})                 {}
func (db *SQLXDB) MustExec(query string, args ...interface{})              {}

func insecureSQLX(db *SQLXDB, user string, dest interface{}) {
	// ruleid: go-injection-sql-injection-sqlx
	db.Queryx(fmt.Sprintf("select * from users where name = '%s'", user))
	// ruleid: go-injection-sql-injection-sqlx
	db.NamedQuery("select * from users where role = '"+user+"'", map[string]any{})
	query := fmt.Sprintf("select * from users where id = %s", user)
	// ruleid: go-injection-sql-injection-sqlx
	db.Get(dest, query)
}

func safeSQLX(db *SQLXDB, user string, dest interface{}) {
	// ok: go-injection-sql-injection-sqlx
	db.Queryx("select * from users where name = ?", user)
	// ok: go-injection-sql-injection-sqlx
	db.Get(dest, "select * from users where id = ?", user)
}
