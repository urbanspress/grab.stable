package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateWorkerControl(t *testing.T) {
	c := &workerControl{token: "host-secret", workers: []*processWorker{{}}}
	r := httptest.NewRequest("GET", "http://localhost/control?worker=0", nil)
	w := httptest.NewRecorder()
	c.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r.Header.Set("Authorization", "Bearer host-secret")
	w = httptest.NewRecorder()
	c.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "run" {
		t.Fatal(w.Code, w.Body.String())
	}
	c.workers[0].drain = true
	w = httptest.NewRecorder()
	c.ServeHTTP(w, r)
	if w.Body.String() != "drain" {
		t.Fatal(w.Body.String())
	}
	r = httptest.NewRequest("GET", "http://localhost/_ready", nil)
	r.Header.Set("Authorization", "Bearer host-secret")
	w = httptest.NewRecorder()
	c.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	for n := 1; n <= 20; n++ {
		d := restartDelay(n)
		if d < time.Second || d > 30*time.Second {
			t.Fatal(d)
		}
	}
}
func TestWorkerSnapshotDetectsChanges(t *testing.T) {
	if _, e := workerMaintenanceSnapshot([]string{""}); e != nil {
		t.Fatal("optional database manifest must not be read as an empty path", e)
	}
	root := t.TempDir()
	program := filepath.Join(root, "app.grab")
	contract := filepath.Join(root, "grab.api.json")
	os.WriteFile(program, []byte("show 1\n"), 0600)
	os.WriteFile(contract, []byte(`{"version":1}`), 0600)
	first, e := SourceHash(program, nil)
	if e != nil {
		t.Fatal(e)
	}
	second, e := SourceHash(program, nil)
	if e != nil || first != second {
		t.Fatal("unstable snapshot")
	}
	os.WriteFile(contract, []byte(`{"version":2}`), 0600)
	third, e := SourceHash(program, nil)
	if e != nil || third == first {
		t.Fatal("contract change missed")
	}
	a, e := contractSnapshot(contract)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(contract, []byte(`{"version":3}`), 0600); e != nil {
		t.Fatal(e)
	}
	b, e := contractSnapshot(contract)
	if e != nil || a == b {
		t.Fatal("rolling contract compatibility guard missed changed bytes")
	}
}
