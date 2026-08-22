package chatstorage

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Dialect identifies the SQL flavour behind the chat storage *sql.DB.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// detectDialect sniffs the driver type behind db. lib/pq registers *pq.Driver,
// pgx/stdlib registers *stdlib.Driver; everything else is treated as SQLite.
func detectDialect(db *sql.DB) Dialect {
	if db == nil {
		return DialectSQLite
	}
	name := fmt.Sprintf("%T", db.Driver())
	if strings.Contains(name, "pq.") || strings.Contains(name, "pgx") || strings.Contains(name, "stdlib.") {
		return DialectPostgres
	}
	return DialectSQLite
}

// dbConn wraps *sql.DB so every query written with SQLite-style `?`
// placeholders is rewritten to `$1..$n` when the backing database is Postgres.
// Call sites in the repository stay dialect-agnostic.
type dbConn struct {
	*sql.DB
	dialect Dialect
}

type dbTx struct {
	*sql.Tx
	dialect Dialect
}

func newDBConn(db *sql.DB, dialect Dialect) *dbConn {
	return &dbConn{DB: db, dialect: dialect}
}

// rewrite converts `?` placeholders to `$n` for Postgres. It skips `?` inside
// single-quoted string literals so queries like `LIKE '%?%'` stay intact.
func rewrite(dialect Dialect, query string) string {
	if dialect != DialectPostgres || !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 0
	inQuote := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
			b.WriteByte(c)
		case c == '?' && !inQuote:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (c *dbConn) Exec(query string, args ...any) (sql.Result, error) {
	return c.DB.Exec(rewrite(c.dialect, query), args...)
}

func (c *dbConn) Query(query string, args ...any) (*sql.Rows, error) {
	return c.DB.Query(rewrite(c.dialect, query), args...)
}

func (c *dbConn) QueryRow(query string, args ...any) *sql.Row {
	return c.DB.QueryRow(rewrite(c.dialect, query), args...)
}

func (c *dbConn) Prepare(query string) (*sql.Stmt, error) {
	return c.DB.Prepare(rewrite(c.dialect, query))
}

func (c *dbConn) Begin() (*dbTx, error) {
	tx, err := c.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &dbTx{Tx: tx, dialect: c.dialect}, nil
}

func (t *dbTx) Exec(query string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(rewrite(t.dialect, query), args...)
}

func (t *dbTx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(rewrite(t.dialect, query), args...)
}

func (t *dbTx) QueryRow(query string, args ...any) *sql.Row {
	return t.Tx.QueryRow(rewrite(t.dialect, query), args...)
}

func (t *dbTx) Prepare(query string) (*sql.Stmt, error) {
	return t.Tx.Prepare(rewrite(t.dialect, query))
}

// translateDDL adapts the SQLite-flavoured migration DDL to Postgres.
func translateDDL(dialect Dialect, ddl string) string {
	if dialect != DialectPostgres {
		return ddl
	}
	r := strings.NewReplacer(
		"INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY",
		" BLOB", " BYTEA",
		"BOOLEAN NOT NULL DEFAULT 1", "BOOLEAN NOT NULL DEFAULT TRUE",
		"BOOLEAN NOT NULL DEFAULT 0", "BOOLEAN NOT NULL DEFAULT FALSE",
		"BOOLEAN DEFAULT 1", "BOOLEAN DEFAULT TRUE",
		"BOOLEAN DEFAULT 0", "BOOLEAN DEFAULT FALSE",
		// Postgres ALTER TABLE ADD COLUMN is not idempotent without IF NOT EXISTS.
		"ADD COLUMN ", "ADD COLUMN IF NOT EXISTS ",
	)
	return r.Replace(ddl)
}

// caseInsensitiveLike returns the LIKE operator that matches case-insensitively
// on the current dialect (SQLite LIKE is already case-insensitive for ASCII).
func (c *dbConn) caseInsensitiveLike() string {
	if c.dialect == DialectPostgres {
		return "ILIKE"
	}
	return "LIKE"
}
