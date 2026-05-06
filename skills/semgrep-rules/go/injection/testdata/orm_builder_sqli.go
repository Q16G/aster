package testdata

import (
	"fmt"

	sql "entgo.io/ent/dialect/sql"
)

type PopConn struct{}
type QueryBuilder struct{}
type Selector struct{}
type ReformDB struct{}

func (c *PopConn) RawQuery(query string, args ...interface{}) {}
func (c *PopConn) Q() *QueryBuilder                           { return &QueryBuilder{} }
func (q *QueryBuilder) Where(query string, args ...interface{}) {}
func (q *QueryBuilder) Order(clause string)                     {}
func (q *QueryBuilder) OrderBy(clause string)                   {}
func (db *ReformDB) Exec(query string, args ...interface{})    {}
func (s *Selector) Where(expr interface{})                     {}
func (s *Selector) OrderExpr(expr interface{})                 {}

func insecureORM(pop *PopConn, db *ReformDB, selector *Selector, input string) {
	// ruleid: go-injection-sql-injection-orm-builders
	pop.RawQuery("select * from users where role = '"+input+"'")
	q := pop.Q()
	// ruleid: go-injection-sql-injection-orm-builders
	q.Where("name = '" + input + "'")
	// ruleid: go-injection-sql-injection-orm-builders
	q.Order("created_at " + input)
	// ruleid: go-injection-sql-injection-orm-builders
	db.Exec(fmt.Sprintf("delete from audit where actor = '%s'", input))
	// ruleid: go-injection-sql-injection-orm-builders
	selector.Where(sql.ExprP(input))
}

func safeORM(pop *PopConn, selector *Selector, input string) {
	// ok: go-injection-sql-injection-orm-builders
	pop.RawQuery("select * from users where role = ?", input)
	// ok: go-injection-sql-injection-orm-builders
	selector.Where(sql.EQ("role", input))
}
