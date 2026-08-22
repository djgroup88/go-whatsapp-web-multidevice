package chatstorage

import "testing"

func TestRewritePlaceholders(t *testing.T) {
	cases := map[string]string{
		"SELECT 1":                               "SELECT 1",
		"WHERE a = ? AND b = ?":                  "WHERE a = $1 AND b = $2",
		"WHERE a = ? AND s = 'x?y' AND b = ?":    "WHERE a = $1 AND s = 'x?y' AND b = $2",
		"IN (?,?,?)":                             "IN ($1,$2,$3)",
		"(? = 1 AND x = 0) AND (? = 0 OR y = ?)": "($1 = 1 AND x = 0) AND ($2 = 0 OR y = $3)",
	}
	for in, want := range cases {
		if got := rewrite(DialectPostgres, in); got != want {
			t.Errorf("rewrite(%q) = %q, want %q", in, got, want)
		}
		if got := rewrite(DialectSQLite, in); got != in {
			t.Errorf("sqlite rewrite(%q) should be untouched, got %q", in, got)
		}
	}
}

func TestTranslateDDL(t *testing.T) {
	in := "CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, k BLOB, e BOOLEAN NOT NULL DEFAULT 1, f BOOLEAN DEFAULT FALSE)"
	want := "CREATE TABLE t (id BIGSERIAL PRIMARY KEY, k BYTEA, e BOOLEAN NOT NULL DEFAULT TRUE, f BOOLEAN DEFAULT FALSE)"
	if got := translateDDL(DialectPostgres, in); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	alter := "ALTER TABLE chats ADD COLUMN archived BOOLEAN DEFAULT FALSE;"
	if got := translateDDL(DialectPostgres, alter); got != "ALTER TABLE chats ADD COLUMN IF NOT EXISTS archived BOOLEAN DEFAULT FALSE;" {
		t.Errorf("alter: %s", got)
	}
	if got := translateDDL(DialectSQLite, in); got != in {
		t.Errorf("sqlite DDL must be untouched")
	}
}
