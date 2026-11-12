//go:build (windows || linux) && amd64

package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"grab/internal/data"
	"grab/internal/interpreter"
	rt "grab/internal/runtime"
)

func TestCatalogDatabaseLifecycle(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"catalog_app.grab", "catalog_migrate.grab", "catalog_seed.grab", "catalog_home.grab.html", "catalog_product.grab.html"} {
		b, e := os.ReadFile(filepath.Join("..", "..", "examples", name))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	c, e := data.ParseConfig([]byte(`{"connections":{"shop":{"driver":"sqlite","path":"catalog.db"}}}`), filepath.Join(dir, "grab.data.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range []struct{ name, mode string }{{"catalog_migrate.grab", "migrate"}, {"catalog_seed.grab", "read-write"}} {
		grants, e := c.Grant([]string{"shop:" + entry.mode}, false)
		if e != nil {
			t.Fatal(e)
		}
		p, e := readProgram(filepath.Join(dir, entry.name))
		if e != nil {
			t.Fatal(e)
		}
		if e = interpreter.RunWithOptions(p, io.Discard, interpreter.Options{DataGrants: grants}); e != nil {
			t.Fatal(e)
		}
	}
	grants, e := c.Grant([]string{"shop:read"}, true)
	if e != nil {
		t.Fatal(e)
	}
	appPath := filepath.Join(dir, "catalog_app.grab")
	a, e := NewApplication(appPath, "", interpreter.Options{DataGrants: grants}, DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	for _, tt := range []struct {
		path, want string
		status     int
	}{{"/", "2 products stored in SQLite", 200}, {"/products/1", "Jasmine tea", 200}, {"/api/products", "18.95", 200}, {"/products/999", "Product not found", 404}} {
		w := request(a, "GET", tt.path, "")
		if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.want) {
			t.Fatalf("%s: %d %s", tt.path, w.Code, w.Body)
		}
	}
	old := a.active.Load()
	var requests sync.WaitGroup
	failures := make(chan string, 16)
	for i := 0; i < 16; i++ {
		requests.Add(1)
		go func(i int) {
			defer requests.Done()
			path, want := "/products/1", "Jasmine tea"
			if i%2 == 1 {
				path, want = "/products/2", "Coffee beans"
			}
			w := request(a, "GET", path, "")
			if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
				failures <- w.Body.String()
			}
		}(i)
	}
	requests.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal("simultaneous request lost isolated result", failure)
	}
	if !old.acquire() {
		t.Fatal("could not retain old generation")
	}
	if e = a.Reload(); e != nil {
		t.Fatal(e)
	}
	var route *interpreter.Route
	for _, r := range old.code.Routes() {
		if r.Definition.Path == "/" {
			route = r
		}
	}
	if _, _, e = old.code.Execute(context.Background(), route, rt.Record(nil), io.Discard); e != nil {
		t.Fatalf("old request did not drain: %v", e)
	}
	old.release()
	if _, _, e = old.code.Execute(context.Background(), route, rt.Record(nil), io.Discard); e == nil {
		t.Fatal("retired resources remained usable")
	}
	if e = os.WriteFile(appPath, []byte("use data\nset db to data.open using \"shop\"\nset broken to data.prepare using db, \"SELECT missing FROM nonexistent\", record {params: record {}, columns: record {missing: \"text\"}}\nroute \"GET\" \"/\"\n  give back \"broken\"\nend\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = a.Reload(); e == nil {
		t.Fatal("invalid startup query replaced live application")
	}
	if w := request(a, "GET", "/products/1", ""); w.Code != 200 {
		t.Fatalf("failed reload lost old app: %d %s", w.Code, w.Body)
	}
	w := request(a, "GET", "/products/not_an_integer", "")
	if w.Code != 500 || strings.Contains(w.Body.String(), "SELECT") || strings.Contains(w.Body.String(), "catalog.db") {
		t.Fatal("HTTP database details leaked", w.Body)
	}
	a.Close()
	if e = a.Reload(); e == nil {
		t.Fatal("closed application reloaded resources")
	}
	writeGrants, e := c.Grant([]string{"shop:read-write"}, true)
	if e != nil {
		t.Fatal(e)
	}
	rollbackPath := filepath.Join(dir, "rollback_app.grab")
	source := `use data
use server
set db to data.open using "shop"
set insert to data.prepare using db, "INSERT INTO products(id,name,price) VALUES (9999,'Uncommitted','1.00')", record {params: record {}, columns: record {}}
set count_rows to data.prepare using db, "SELECT count(id) AS n FROM products WHERE id=9999", record {params: record {}, columns: record {n: "integer"}}
route "POST" "/forgot"
  set tx to data.begin using db
  call data.execute using insert, record {}, tx
  give back server.text using 200, "Transaction deliberately unfinished"
end
route "GET" "/count"
  set row to data.one using count_rows, record {}
  give back server.json using 200, row.n
end
`
	if e = os.WriteFile(rollbackPath, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	writer, e := NewApplication(rollbackPath, "", interpreter.Options{DataGrants: writeGrants}, DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Close()
	for i := 0; i < 2; i++ {
		if w := request(writer, "POST", "/forgot", ""); w.Code != 200 {
			t.Fatal("request leaked transaction across executions", w.Body)
		}
		if w := request(writer, "GET", "/count", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "0" {
			t.Fatal("unfinished request transaction persisted", w.Body)
		}
	}
}
