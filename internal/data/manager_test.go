package data

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rt "grab/internal/runtime"
)

func testManager(t *testing.T, driver string) (*Manager, *Scope, *Pool) {
	t.Helper()
	definition := `{"driver":"sqlite","path":":memory:"}`
	if driver != "sqlite" {
		if os.Getenv("GRAB_TEST_DATABASES") != "1" {
			t.Skip("real-service suite: set GRAB_TEST_DATABASES=1 with documented disposable containers")
		}
		port := 55432
		user := "grab"
		if driver == "mariadb" {
			port = 53306
			user = "grab"
		}
		definition = fmt.Sprintf(`{"driver":%q,"host":"127.0.0.1","port":%d,"database":"grab_test","user":%q,"password_env":"GRAB_TEST_DB_PASSWORD","tls":"disabled"}`, driver, port, user)
	}
	c, e := ParseConfig([]byte(`{"connections":{"shop":`+definition+`}}`), filepath.Join(t.TempDir(), "grab.data.json"))
	if e != nil {
		t.Fatal(e)
	}
	grants, e := c.Grant([]string{"shop:migrate"}, false)
	if e != nil {
		t.Fatal(e)
	}
	m := NewManager(grants, false)
	scope := m.Scope()
	t.Cleanup(func() {
		scope.Close()
		if e := m.Close(); e != nil {
			t.Error(e)
		}
	})
	pool, e := scope.Open(context.Background(), "shop")
	if e != nil {
		t.Fatal(e)
	}
	return m, scope, pool
}

func TestRealAdapterConformance(t *testing.T) {
	for _, driver := range []string{"sqlite", "mariadb", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			_, scope, pool := testManager(t, driver)
			ctx := context.Background()
			priceType := "TEXT"
			storage := "text"
			if driver != "sqlite" {
				priceType = "DECIMAL(24,2)"
				storage = ""
			}
			manifest, e := MigrationManifest(rt.List([]rt.Value{migrationValue(1, "create_products", "CREATE TABLE grab07_products (id BIGINT PRIMARY KEY, name TEXT NOT NULL, price "+priceType+" NOT NULL)")}), driver)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = scope.Migrate(ctx, pool, manifest); e != nil {
				t.Fatal(e)
			}
			again, e := scope.Migrate(ctx, pool, manifest)
			if e != nil {
				t.Fatal(e)
			}
			applied, _ := again.LookupEntry("applied")
			if applied.ListLen() != 0 {
				t.Fatal("identical migration was not a no-op")
			}
			price := TypeSpec{Type: "decimal", Precision: 24, Scale: 2, Storage: storage}
			params := []Field{{"id", TypeSpec{Type: "integer"}}, {"name", TypeSpec{Type: "text"}}, {"price", price}}
			insert, e := scope.Prepare(ctx, pool, "INSERT INTO grab07_products(id,name,price) VALUES (:id,:name,:price)", Contract{Params: params})
			if e != nil {
				t.Fatal(e)
			}
			clear, e := scope.Prepare(ctx, pool, "DELETE FROM grab07_products", Contract{})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = scope.Query(ctx, clear, rt.Record(nil), nil, false, true); e != nil {
				t.Fatal(e)
			}
			payload := "Tea'); DROP TABLE grab07_products; -- 🍵"
			values := rt.Record([]rt.Entry{{Name: "id", Value: rt.IntegerFromInt64(1)}, {Name: "name", Value: rt.Text(payload)}, {Name: "price", Value: decimal(t, "1234567890123456789012.34")}})
			if _, e = scope.Query(ctx, insert, values, nil, false, true); e != nil {
				t.Fatal(e)
			}
			selectStmt, e := scope.Prepare(ctx, pool, "SELECT id, name, price FROM grab07_products ORDER BY id", Contract{Columns: params})
			if e != nil {
				t.Fatal(e)
			}
			one, e := scope.Query(ctx, selectStmt, rt.Record(nil), nil, true, false)
			if e != nil {
				t.Fatal(e)
			}
			name, _ := one.LookupEntry("name")
			amount, _ := one.LookupEntry("price")
			if name.TextValue() != payload || amount.Display() != "1234567890123456789012.34" {
				t.Fatalf("lossy adapter result: %s", one.Display())
			}
			tx, e := scope.Begin(ctx, pool, false, "serializable")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = scope.Query(ctx, clear, rt.Record(nil), tx, false, true); e != nil {
				t.Fatal(e)
			}
			if e = scope.Rollback(tx); e != nil {
				t.Fatal(e)
			}
			if _, e = scope.Query(ctx, selectStmt, rt.Record(nil), nil, true, false); e != nil {
				t.Fatal(e)
			}
			tx, e = scope.Begin(ctx, pool, false, "serializable")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = scope.Query(ctx, insert, values, tx, false, true); e == nil {
				t.Fatal("duplicate primary key succeeded")
			}
			if e = scope.Commit(tx); e == nil {
				t.Fatal("rollback-only transaction committed")
			}
			if e = scope.Rollback(tx); e != nil {
				t.Fatal(e)
			}
			if stats := pool.db.Stats(); stats.InUse != 0 {
				t.Fatalf("connection leaked: %+v", stats)
			}
			testNativeTypes(t, scope, pool, driver)
			if driver != "sqlite" {
				testRemoteCancellationAndReadOnly(t, scope, pool)
			}
		})
	}
}

func testRemoteCancellationAndReadOnly(t *testing.T, s *Scope, p *Pool) {
	t.Helper()
	ctx := context.Background()
	deleteRows, err := s.Prepare(ctx, p, "DELETE FROM grab07_products", Contract{})
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = internalExec(ctx, blocker, "UPDATE grab07_products SET name=name WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = s.Query(deadline, deleteRows, rt.Record(nil), nil, false, true); errorCode(err) != Limit {
		t.Fatal("blocked statement ignored cancellation", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("remote cancellation did not promptly release execution")
	}
	if err = blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = internalExec(ctx, p.db, "SELECT 1"); err != nil {
		t.Fatal("pool unusable after cancellation", err)
	}
	readManager := NewManager(&Grants{config: &Config{connections: map[string]definition{"reader": p.def}}, modes: map[string]Mode{"reader": Read}}, false)
	defer readManager.Close()
	reader := readManager.Scope()
	defer reader.Close()
	readPool, err := reader.Open(ctx, "reader")
	if err != nil {
		t.Fatal(err)
	}
	readPool.db.SetMaxIdleConns(0) // Exercise native read-only setup on reconnects.
	for i := 0; i < 2; i++ {
		if _, err = internalExec(ctx, readPool.db, "DELETE FROM grab07_products"); err == nil {
			t.Fatal("read connection wrote through native engine")
		}
	}
	if stats := p.db.Stats(); stats.InUse != 0 {
		t.Fatal("remote connection leaked after cancellation", stats)
	}
	verified := p.def
	verified.TLS = "verify"
	verifiedManager := NewManager(&Grants{config: &Config{connections: map[string]definition{"verified": verified}}, modes: map[string]Mode{"verified": Read}}, false)
	defer verifiedManager.Close()
	verifiedScope := verifiedManager.Scope()
	defer verifiedScope.Close()
	if _, err = verifiedScope.Open(ctx, "verified"); err == nil {
		t.Fatal("untrusted disposable server unexpectedly passed TLS verification")
	}
}

func testNativeTypes(t *testing.T, s *Scope, p *Pool, driver string) {
	t.Helper()
	ctx := context.Background()
	ddl := "CREATE TABLE IF NOT EXISTS grab07_types (id BIGINT PRIMARY KEY, flag INTEGER, stamp TEXT, naive TEXT, bin BLOB, optional TEXT, approximate REAL)"
	if driver == "mariadb" {
		ddl = "CREATE TABLE IF NOT EXISTS grab07_types (id BIGINT PRIMARY KEY, flag BOOLEAN, stamp TIMESTAMP(6), naive DATETIME(6), bin BLOB, optional TEXT, approximate DOUBLE)"
	}
	if driver == "postgres" {
		ddl = "CREATE TABLE IF NOT EXISTS grab07_types (id BIGINT PRIMARY KEY, flag BOOLEAN, stamp TIMESTAMPTZ(6), naive TIMESTAMP(6), bin BYTEA, optional TEXT, approximate DOUBLE PRECISION)"
	}
	// Schema is a host test fixture, not a public query API escape hatch.
	if p.policy != nil {
		p.policy.Maintenance.Store(true)
	}
	_, e := internalExec(ctx, p.db, ddl)
	if p.policy != nil {
		p.policy.Maintenance.Store(false)
	}
	if e != nil {
		t.Fatal(e)
	}
	fields := []Field{{"id", TypeSpec{Type: "integer"}}, {"flag", TypeSpec{Type: "boolean"}}, {"stamp", TypeSpec{Type: "timestamp", Zone: "utc", FractionDigits: 6}}, {"naive", TypeSpec{Type: "timestamp", Zone: "naive", FractionDigits: 6}}, {"bin", TypeSpec{Type: "bytes"}}, {"optional", TypeSpec{Type: "text", Nullable: true}}, {"approximate", TypeSpec{Type: "float"}}}
	clear, e := s.Prepare(ctx, p, "DELETE FROM grab07_types", Contract{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Query(ctx, clear, rt.Record(nil), nil, false, true); e != nil {
		t.Fatal(e)
	}
	insert, e := s.Prepare(ctx, p, "INSERT INTO grab07_types(id,flag,stamp,naive,bin,optional,approximate) VALUES (:id,:flag,:stamp,:naive,:bin,:optional,:approximate)", Contract{Params: fields})
	if e != nil {
		t.Fatal(e)
	}
	f, _ := rt.FloatFromFloat64(1.25)
	values := rt.Record([]rt.Entry{{Name: "id", Value: rt.IntegerFromInt64(1)}, {Name: "flag", Value: rt.Boolean(false)}, {Name: "stamp", Value: rt.Text("2026-11-11T01:02:03.123456-05:00")}, {Name: "naive", Value: rt.Text("2026-11-11T01:02:03.123456")}, {Name: "bin", Value: rt.Bytes([]byte{})}, {Name: "optional", Value: rt.Nothing()}, {Name: "approximate", Value: f}})
	if _, e = s.Query(ctx, insert, values, nil, false, true); e != nil {
		t.Fatal(e)
	}
	query, e := s.Prepare(ctx, p, "SELECT id, flag, stamp, naive, bin, optional, approximate FROM grab07_types", Contract{Columns: fields})
	if e != nil {
		t.Fatal(e)
	}
	row, e := s.Query(ctx, query, rt.Record(nil), nil, true, false)
	if e != nil {
		t.Fatal(e)
	}
	stamp, _ := row.LookupEntry("stamp")
	if stamp.TextValue() != "2026-11-11T06:02:03.123456Z" {
		t.Fatal("timestamp lost precision/instant", stamp.Display())
	}
	bin, _ := row.LookupEntry("bin")
	optional, present := row.LookupEntry("optional")
	if bin.Kind() != rt.BytesKind || len(bin.BytesValue()) != 0 || !present || optional.Kind() != rt.NothingKind {
		t.Fatal("empty bytes or NULL lost")
	}
	if driver != "sqlite" {
		dateQuery, err := s.Prepare(ctx, p, "SELECT CAST('2026-11-11' AS DATE) AS day, CAST('12:34:56' AS TIME) AS clock", Contract{Columns: []Field{{"day", TypeSpec{Type: "text"}}, {"clock", TypeSpec{Type: "text"}}}})
		if err != nil {
			t.Fatal(err)
		}
		dateRow, err := s.Query(ctx, dateQuery, rt.Record(nil), nil, true, false)
		if err != nil {
			t.Fatal(err)
		}
		day, _ := dateRow.LookupEntry("day")
		clock, _ := dateRow.LookupEntry("clock")
		if day.TextValue() != "2026-11-11" || clock.TextValue() != "12:34:56" {
			t.Fatal("DATE/TIME were not retained as text", dateRow.Display())
		}
	}
}

func TestTransactionOwnershipAndCleanup(t *testing.T) {
	m, s, p := testManager(t, "sqlite")
	ctx := context.Background()
	tx, e := s.Begin(ctx, p, false, "serializable")
	if e != nil {
		t.Fatal(e)
	}
	other := m.Scope()
	defer other.Close()
	if e = other.Commit(tx); e == nil {
		t.Fatal("cross-scope transaction committed")
	}
	if _, e = s.Begin(ctx, p, false, "serializable"); e == nil {
		t.Fatal("nested transaction accepted")
	}
	s.Close()
	if p.db.Stats().InUse != 0 {
		t.Fatal("forgotten transaction was not rolled back")
	}
	if e = other.ClosePool(p); e != nil {
		t.Fatal(e)
	}
	if e = other.ClosePool(p); e != nil {
		t.Fatal("close is not idempotent", e)
	}
}

func TestPoolWaitIsBounded(t *testing.T) {
	m, s, p := testManager(t, "sqlite")
	ctx := context.Background()
	if _, e := s.Begin(ctx, p, false, "serializable"); e != nil {
		t.Fatal(e)
	}
	other := m.Scope()
	defer other.Close()
	deadline, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e := other.Begin(deadline, p, false, "serializable"); e == nil {
		t.Fatal("exhausted pool ignored deadline")
	}
	if time.Since(start) > time.Second {
		t.Fatal("pool wait not bounded")
	}
}
