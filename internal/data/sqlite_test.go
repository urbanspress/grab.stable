package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"
)

func memoryDB(t *testing.T, policy *SQLitePolicy) *sql.DB {
	t.Helper()
	c, e := SQLiteConnector(":memory:", policy)
	if e != nil {
		t.Fatal(e)
	}
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLiteAuthorization(t *testing.T) {
	p := &SQLitePolicy{}
	db := memoryDB(t, p)
	p.Maintenance.Store(true)
	p.Ledger.Store(true)
	for _, s := range []string{"CREATE TABLE products (id INTEGER PRIMARY KEY, price TEXT, parent INTEGER REFERENCES products(id))", "CREATE TABLE grab_schema_migrations (version INTEGER)"} {
		if _, e := db.Exec(s); e != nil {
			t.Fatal(e)
		}
	}
	p.Maintenance.Store(false)
	p.Ledger.Store(false)
	for _, s := range []string{"INSERT INTO products(id,price) VALUES (1,'1234567890123456.78')", "SELECT id, price FROM products", "SELECT abs(-1)"} {
		stmt, e := db.Prepare(s)
		if e != nil {
			t.Fatalf("%s: %v", s, e)
		}
		_ = stmt.Close()
	}
	for _, s := range []string{"ATTACH DATABASE ':memory:' AS other", "PRAGMA foreign_keys=OFF", "SELECT load_extension('escape')", "CREATE TABLE unauthorized (id INTEGER)", "CREATE VIRTUAL TABLE escape USING fts5(body)", "INSERT INTO grab_schema_migrations VALUES(1)", "UPDATE sqlite_master SET sql='unsafe'", "SELECT writefile('escape','payload')"} {
		t.Run(s, func(t *testing.T) {
			stmt, e := db.Prepare(s)
			if e == nil {
				_ = stmt.Close()
				t.Fatal("engine authorized forbidden SQL")
			}
		})
	}
	if _, e := db.Exec("INSERT INTO products(id,parent) VALUES (2,999)"); e == nil {
		t.Fatal("foreign keys not enforced")
	}
}

func TestSQLiteReadOnlyAndReconnect(t *testing.T) {
	p := &SQLitePolicy{ReadOnly: true}
	db := memoryDB(t, p)
	for i := 0; i < 2; i++ {
		var n int
		if e := db.QueryRow("SELECT 42").Scan(&n); e != nil || n != 42 {
			t.Fatalf("read: %v", e)
		}
		p.Maintenance.Store(true)
		if _, e := db.Exec("CREATE TABLE escape (id INTEGER)"); e == nil {
			t.Fatal("read grant bypassed by maintenance")
		}
		// Force a new physical connection and verify the policy is installed.
		conn, e := db.Conn(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
	}
}

func TestSQLiteExactStorageAndCancellation(t *testing.T) {
	p := &SQLitePolicy{}
	db := memoryDB(t, p)
	p.Maintenance.Store(true)
	if _, e := db.Exec("CREATE TABLE money (exact TEXT, approximate NUMERIC)"); e != nil {
		t.Fatal(e)
	}
	p.Maintenance.Store(false)
	stmt, e := db.Prepare("INSERT INTO money VALUES (?1,?1)")
	if e != nil {
		t.Fatal(e)
	}
	defer stmt.Close()
	if _, e = stmt.Exec("1234567890123456.78"); e != nil {
		t.Fatal(e)
	}
	var exact, approx any
	if e = db.QueryRow("SELECT exact, approximate FROM money").Scan(&exact, &approx); e != nil {
		t.Fatal(e)
	}
	s := TypeSpec{Type: "decimal", Precision: 18, Scale: 2, Storage: "text"}
	if _, e = s.Decode(exact, "sqlite"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Decode(approx, "sqlite"); e == nil {
		t.Fatal("NUMERIC coercion accepted as exact money")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	var n int
	e = db.QueryRowContext(ctx, "WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n) SELECT sum(x) FROM n").Scan(&n)
	if e == nil {
		t.Fatal("unbounded query was not canceled")
	}
	if time.Since(started) > time.Second {
		t.Fatal("cancellation did not release connection promptly")
	}
	if e = db.QueryRow("SELECT 7").Scan(&n); e != nil || n != 7 {
		t.Fatalf("connection unusable after cancellation: %v", e)
	}
}
