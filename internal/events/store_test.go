package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"grab/internal/data"
	"grab/internal/platform"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func storeFixture(t *testing.T) (*Store, *int64) {
	t.Helper()
	if driver := os.Getenv("GRAB_EVENT_ENGINE"); driver != "" && driver != "sqlite" {
		return remoteStoreFixture(t, driver)
	}
	policy := &data.SQLitePolicy{}
	policy.Maintenance.Store(true)
	connector, e := data.SQLiteConnector(":memory:", policy)
	if e != nil {
		t.Fatal(e)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	for _, step := range MigrationSteps("sqlite") {
		if _, e := db.Exec(step); e != nil {
			t.Fatal(step, e)
		}
	}
	if _, e = db.Exec("CREATE TABLE business (id TEXT PRIMARY KEY)"); e != nil {
		t.Fatal(e)
	}
	policy.Maintenance.Store(false)
	now := int64(1000000)
	s := &Store{DB: db, Driver: "sqlite", Config: platform.EventStore{Namespace: "test", MaxPending: 100, MaxBytes: 1 << 20, MaxAttempts: 8}, Now: func() time.Time { return time.UnixMilli(now) }}
	if e = s.InitNamespace(context.Background()); e != nil {
		t.Fatal(e)
	}
	return s, &now
}
func inbox() Work {
	return Work{Kind: "inbox", Connector: "reference", Tenant: "a", EventID: "evt_1", Type: "notification", Payload: []byte(`{"message":"hello"}`)}
}
func mustClaim(t *testing.T, s *Store, owner string) Work {
	t.Helper()
	w, e := s.Claim(context.Background(), owner)
	if e != nil || w.ID == "" {
		t.Fatal("claim", w, e)
	}
	return w
}
func TestDurableAcceptance(t *testing.T) {
	s, _ := storeFixture(t)
	ctx := context.Background()
	w := inbox()
	id, duplicate, e := s.Accept(ctx, w)
	if e != nil || duplicate || id == "" {
		t.Fatal(id, duplicate, e)
	}
	same, duplicate, e := s.Accept(ctx, w)
	if e != nil || !duplicate || same != id {
		t.Fatal(same, duplicate, e)
	}
	w.Payload = []byte(`{"message":"changed"}`)
	if _, _, e = s.Accept(ctx, w); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	w.Tenant = "b"
	if _, duplicate, e = s.Accept(ctx, w); e != nil || duplicate {
		t.Fatal("tenant isolation", e)
	}
	s.Config.MaxPending = 2
	w.EventID = "evt_2"
	if _, _, e = s.Accept(ctx, w); !errors.Is(e, ErrFull) {
		t.Fatal("capacity", e)
	}
	var count int
	s.DB.QueryRow("SELECT COUNT(*) FROM grab_events_work").Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
}
func TestFencedBusinessOutboxCommitAndRollback(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[complete], func(t *testing.T) {
			s, _ := storeFixture(t)
			ctx := context.Background()
			id, _, e := s.Accept(ctx, inbox())
			if e != nil {
				t.Fatal(e)
			}
			w := mustClaim(t, s, "worker")
			tx, e := s.DB.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Fence(ctx, tx, w); e != nil {
				t.Fatal(e)
			}
			if _, e = s.exec(ctx, tx, "INSERT INTO business(id) VALUES(?)", id); e != nil {
				t.Fatal(e)
			}
			out, e := s.Enqueue(ctx, tx, Work{Kind: "outbox", Connector: "reference", Operation: "send", Payload: []byte(`{"message":"hello"}`), Key: "stable-key", Classification: "idempotent"})
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Complete(ctx, tx, w); e != nil {
				t.Fatal(e)
			}
			if complete {
				e = tx.Commit()
			} else {
				e = tx.Rollback()
			}
			if e != nil {
				t.Fatal(e)
			}
			status, e := s.Inspect(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			var count int
			s.DB.QueryRow("SELECT COUNT(*) FROM business").Scan(&count)
			if complete {
				if status.State != "completed" || count != 1 {
					t.Fatal(status, count)
				}
				v, e := s.Inspect(ctx, out)
				if e != nil || v.State != "pending" {
					t.Fatal(v, e)
				}
			} else {
				if status.State != "leased" || count != 0 {
					t.Fatal(status, count)
				}
				if _, e = s.Inspect(ctx, out); e == nil {
					t.Fatal("outbox survived rollback")
				}
			}
		})
	}
}
func TestLeaseExpiryFencesStaleWorker(t *testing.T) {
	s, now := storeFixture(t)
	ctx := context.Background()
	s.Accept(ctx, inbox())
	old := mustClaim(t, s, "old")
	*now += 30001
	live := mustClaim(t, s, "new")
	if live.Token != old.Token+1 || live.Attempts != 2 {
		t.Fatal(live)
	}
	if s.Renew(ctx, old) != ErrFence {
		t.Fatal("stale renewal")
	}
	tx, _ := s.DB.BeginTx(ctx, nil)
	if e := s.Complete(ctx, tx, old); e != ErrFence {
		t.Fatal(e)
	}
	tx.Rollback()
	tx, _ = s.DB.BeginTx(ctx, nil)
	if e := s.Complete(ctx, tx, live); e != nil {
		t.Fatal(e)
	}
	if e := tx.Commit(); e != nil {
		t.Fatal(e)
	}
	var pending int
	s.DB.QueryRow("SELECT pending FROM grab_events_budget").Scan(&pending)
	if pending != 0 {
		t.Fatal(pending)
	}
}
func TestUncertainRecoveryAndAuditedResolution(t *testing.T) {
	s, now := storeFixture(t)
	ctx := context.Background()
	tx, _ := s.DB.BeginTx(ctx, nil)
	id, e := s.Enqueue(ctx, tx, Work{Kind: "outbox", Connector: "reference", Operation: "send", Key: "keep-this-key", Payload: []byte(`{}`), Classification: "unsafe"})
	if e != nil {
		t.Fatal(e)
	}
	tx.Commit()
	w := mustClaim(t, s, "crashed-after-send")
	*now += 30001
	if reclaimed, e := s.Claim(ctx, "replacement"); e != nil || reclaimed.ID != "" {
		t.Fatal(reclaimed, e)
	}
	v, _ := s.Inspect(ctx, id)
	if v.State != "uncertain" {
		t.Fatal(v, w)
	}
	if s.Maintain(ctx, id, "retry", "", "reason") == nil {
		t.Fatal("blind uncertain retry")
	}
	if e = s.Maintain(ctx, id, "resolve", "not-delivered", "provider confirmed no effect"); e != nil {
		t.Fatal(e)
	}
	v, _ = s.Inspect(ctx, id)
	if v.State != "uncertain" {
		t.Fatal("resolution auto-sent", v)
	}
	if e = s.Maintain(ctx, id, "retry", "", "operator approved retry"); e != nil {
		t.Fatal(e)
	}
	next := mustClaim(t, s, "retried")
	if next.Key != "keep-this-key" {
		t.Fatal(next)
	}
	var audit int
	s.DB.QueryRow("SELECT COUNT(*) FROM grab_events_audit").Scan(&audit)
	if audit != 2 {
		t.Fatal(audit)
	}
}
func TestRetryDeadlineAndRawDigest(t *testing.T) {
	s, now := storeFixture(t)
	ctx := context.Background()
	w := inbox()
	_, _, e := s.AcceptRaw(ctx, w, []byte(`{ "message":"hello" }`))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.AcceptRaw(ctx, w, w.Payload); e != ErrConflict {
		t.Fatal("raw mismatch", e)
	}
	claimed := mustClaim(t, s, "worker")
	if e = s.Fail(ctx, claimed, true, false, 3*time.Second); e != nil {
		t.Fatal(e)
	}
	empty, e := s.Claim(ctx, "early")
	if e != nil || empty.ID != "" {
		t.Fatal(empty, e)
	}
	*now += 3000
	claimed = mustClaim(t, s, "later")
	if e = s.Fail(ctx, claimed, true, false, 25*time.Hour); e != nil {
		t.Fatal(e)
	}
	v, _ := s.Inspect(ctx, claimed.ID)
	if v.State != "dead" {
		t.Fatal(v)
	}
	var pending int
	s.DB.QueryRow("SELECT pending FROM grab_events_budget").Scan(&pending)
	if pending != 0 {
		t.Fatal("budget not released", pending)
	}
}

func remoteStoreFixture(t *testing.T, driver string) (*Store, *int64) {
	t.Helper()
	port := 55432
	if driver == "mariadb" {
		port = 53306
	}
	raw := fmt.Sprintf(`{"connections":{"shop":{"driver":%q,"host":"127.0.0.1","port":%d,"database":"grab_test","user":"grab","password_env":"GRAB_TEST_DB_PASSWORD","tls":"disabled"}}}`, driver, port)
	cfg, e := data.ParseConfig([]byte(raw), filepath.Join(t.TempDir(), "grab.data.json"))
	if e != nil {
		t.Fatal(e)
	}
	grants, e := cfg.Grant([]string{"shop:migrate"}, false)
	if e != nil {
		t.Fatal(e)
	}
	manager := data.NewManager(grants, false)
	scope := manager.Scope()
	pool, e := scope.Open(context.Background(), "shop")
	if e != nil {
		t.Fatal(e)
	}
	db, e := scope.HostMaintenanceDatabase(pool)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"grab_events_audit", "grab_events_work", "grab_events_budget", "grab_events_migrations", "business"} {
		if _, e = db.Exec("DROP TABLE IF EXISTS " + name); e != nil {
			t.Fatal(e)
		}
	}
	if e = scope.MigrateEvents(context.Background(), pool, MigrationSteps(driver)); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE business (id VARCHAR(64) PRIMARY KEY)"); e != nil {
		t.Fatal(e)
	}
	now := int64(1000000)
	s := &Store{DB: db, Driver: driver, Config: platform.EventStore{Namespace: "test", MaxPending: 100, MaxBytes: 1 << 20, MaxAttempts: 8}, Now: func() time.Time { return time.UnixMilli(now) }}
	if e = s.InitNamespace(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = s.Ready(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { scope.Close(); manager.Close() })
	return s, &now
}
