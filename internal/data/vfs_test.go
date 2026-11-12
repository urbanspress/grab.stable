//go:build (windows || linux) && amd64

package data

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func openVFSDB(t *testing.T, v *confinedVFS, mode string, p *SQLitePolicy) *sql.DB {
	t.Helper()
	c, e := SQLiteConnector("file:"+v.base+"?vfs="+v.name+"&mode="+mode+"&_defensive=true", p)
	if e != nil {
		t.Fatal(e)
	}
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db
}

func TestConfinedVFSWriterConflict(t *testing.T) {
	v, e := newConfinedVFS(filepath.Join(t.TempDir(), "conflict.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	p := &SQLitePolicy{}
	p.Maintenance.Store(true)
	a := openVFSDB(t, v, "rwc", p)
	defer a.Close()
	b := openVFSDB(t, v, "rwc", p)
	defer b.Close()
	if _, e = a.Exec("CREATE TABLE products(id INTEGER)"); e != nil {
		t.Fatal(e)
	}
	tx, e := a.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO products VALUES(1)"); e != nil {
		t.Fatal(e)
	}
	if _, e = b.Exec("INSERT INTO products VALUES(2)"); e == nil {
		t.Fatal("concurrent writer escaped SQLite lock")
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	if _, e = b.Exec("INSERT INTO products VALUES(3)"); e != nil {
		t.Fatal("lock not released", e)
	}
}

func TestConfinedVFSSidecarEscape(t *testing.T) {
	for _, kind := range []string{"hardlink", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside")
			if e := os.WriteFile(outside, []byte("protected"), 0600); e != nil {
				t.Fatal(e)
			}
			v, e := newConfinedVFS(filepath.Join(dir, "catalog.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer v.Close()
			p := &SQLitePolicy{}
			p.Maintenance.Store(true)
			db := openVFSDB(t, v, "rwc", p)
			defer db.Close()
			if _, e = db.Exec("CREATE TABLE products(id INTEGER)"); e != nil {
				t.Fatal(e)
			}
			link := os.Link
			if kind == "symlink" {
				link = os.Symlink
			}
			if e = link(outside, filepath.Join(dir, "catalog.db-journal")); e != nil {
				t.Skipf("OS cannot create this test link: %v", e)
			}
			if _, e = db.Exec("INSERT INTO products VALUES(1)"); e == nil {
				t.Fatal("opened hostile sidecar")
			}
			raw, e := os.ReadFile(outside)
			if e != nil || string(raw) != "protected" {
				t.Fatal("outside sidecar target changed", e)
			}
		})
	}
}

func TestConfinedVFSCrashRecovery(t *testing.T) {
	if path := os.Getenv("GRAB_VFS_CRASH_FIXTURE"); path != "" {
		v, e := newConfinedVFS(path)
		if e != nil {
			t.Fatal(e)
		}
		p := &SQLitePolicy{}
		p.Maintenance.Store(true)
		db := openVFSDB(t, v, "rwc", p)
		if _, e = db.Exec("CREATE TABLE products(id INTEGER, value TEXT)"); e != nil {
			t.Fatal(e)
		}
		tx, e := db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 100; i++ {
			if _, e = tx.Exec("INSERT INTO products VALUES(?,?)", i, strings.Repeat("x", 65536)); e != nil {
				t.Fatal(e)
			}
		}
		// Terminate without deferred connection cleanup, leaving a hot journal.
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "crash.db")
	child := exec.Command(os.Args[0], "-test.run=^TestConfinedVFSCrashRecovery$")
	child.Env = append(os.Environ(), "GRAB_VFS_CRASH_FIXTURE="+path)
	if out, e := child.CombinedOutput(); e != nil {
		t.Fatalf("crash fixture: %s %v", out, e)
	}
	if st, e := os.Stat(path + "-journal"); e != nil || st.Size() == 0 {
		t.Fatal("fixture did not leave a rollback journal", e)
	}
	v, e := newConfinedVFS(path)
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	db := openVFSDB(t, v, "rwc", &SQLitePolicy{})
	defer db.Close()
	var count int
	if e = db.QueryRow("SELECT count(id) FROM products").Scan(&count); e != nil || count != 0 {
		t.Fatalf("uncommitted crash data survived: count=%d error=%v", count, e)
	}
}

func TestConfinedVFSPersistenceAndTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	v, e := newConfinedVFS(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := v.Close(); e != nil {
			t.Error(e)
		}
	}()
	p := &SQLitePolicy{}
	db := openVFSDB(t, v, "rwc", p)
	p.Maintenance.Store(true)
	if _, e = db.Exec("CREATE TABLE products(id INTEGER PRIMARY KEY, name TEXT)"); e != nil {
		_ = db.Close()
		t.Fatal(e)
	}
	p.Maintenance.Store(false)
	if _, e = db.Exec("INSERT INTO products VALUES(1,'Tea')"); e != nil {
		t.Fatal(e)
	}
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec("INSERT INTO products VALUES(2,'Coffee')"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	read := openVFSDB(t, v, "ro", &SQLitePolicy{ReadOnly: true})
	defer read.Close()
	var count int
	if e = read.QueryRow("SELECT count(*) FROM products").Scan(&count); e != nil || count != 1 {
		t.Fatalf("reopened count=%d error=%v", count, e)
	}
	if _, e = read.Exec("INSERT INTO products VALUES(3,'Escape')"); e == nil {
		t.Fatal("read-only connection wrote data")
	}
}

func TestConfinedVFSRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.db")
	if e := os.WriteFile(outside, []byte("do not modify"), 0600); e != nil {
		t.Fatal(e)
	}
	v, e := newConfinedVFS(filepath.Join(dir, "catalog.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	for _, path := range []string{"../outside.db", "/elsewhere/catalog.db", "catalog.db-wal", "catalog.db-shm", "file:catalog.db", "catalog.db/other"} {
		if _, ok := v.nameFor(path); ok {
			t.Fatalf("authorized %s", path)
		}
	}
	if e = os.Link(outside, filepath.Join(dir, "catalog.db")); e != nil {
		t.Fatal(e)
	}
	db := openVFSDB(t, v, "rwc", &SQLitePolicy{})
	if e = db.Ping(); e == nil {
		_ = db.Close()
		t.Fatal("opened unauthorized hard link")
	}
	_ = db.Close()
	contents, e := os.ReadFile(outside)
	if e != nil || string(contents) != "do not modify" {
		t.Fatal("outside file was modified")
	}
}
