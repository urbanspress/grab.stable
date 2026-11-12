package main

import (
	"bytes"
	"context"
	"grab/internal/data"
	"grab/internal/events"
	"grab/internal/platform"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineClientCommand(t *testing.T) {
	contract, err := filepath.Abs("../../tests/fixtures/typed-client/grab.api.json")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "client")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"generate", "client", "--api-contract", contract, "--out", out, "--react"}, &stdout, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	for _, name := range []string{"types.ts", "client.ts", "validators.ts", "contract.json", "README", "react.ts", ".grab-generated.json"} {
		if _, e := os.Stat(filepath.Join(out, name)); e != nil {
			t.Fatal(e)
		}
	}
	b, _ := os.ReadFile(filepath.Join(out, "types.ts"))
	if !strings.Contains(string(b), "GrabInteger = string") || !strings.Contains(string(b), "GrabDecimal = string") {
		t.Fatal(string(b))
	}
	os.WriteFile(filepath.Join(out, "client.ts"), []byte("handwritten"), 0600)
	if run([]string{"generate", "client", "--api-contract", contract, "--out", out}, &stdout, &stderr) == 0 {
		t.Fatal("overwrote handwritten code")
	}
}
func TestEventsMaintenanceCommands(t *testing.T) {
	dir := t.TempDir()
	platformPath := filepath.Join(dir, "grab.platform.json")
	dbPath := filepath.Join(dir, "grab.data.json")
	os.WriteFile(platformPath, []byte(`{"version":1,"events":{"queue":{"database":"shop","namespace":"cli"}}}`), 0600)
	raw := []byte(`{"connections":{"shop":{"driver":"sqlite","path":"events.db"}}}`)
	os.WriteFile(dbPath, raw, 0600)
	common := []string{"--platform-config", platformPath, "--db-config", dbPath, "--allow-events", "queue:work"}
	var stdout, stderr bytes.Buffer
	args := append([]string{"migrate", "--events-store", "queue", "--allow-db", "shop:migrate"}, common...)
	if n := run(args, &stdout, &stderr); n != 0 {
		t.Fatal(n, stderr.String())
	}
	if n := run(args, &stdout, &stderr); n != 0 {
		t.Fatal("idempotent migration", n, stderr.String())
	}
	cfg, e := data.ParseConfig(raw, dbPath)
	if e != nil {
		t.Fatal(e)
	}
	g, _ := cfg.Grant([]string{"shop:read-write"}, false)
	m := data.NewManager(g, false)
	defer m.Close()
	sc := m.Scope()
	defer sc.Close()
	p, e := sc.Open(context.Background(), "shop")
	if e != nil {
		t.Fatal(e)
	}
	db, e := sc.HostDatabase(p)
	if e != nil {
		t.Fatal(e)
	}
	store := &events.Store{DB: db, Driver: "sqlite", Config: platform.EventStore{Namespace: "cli", MaxPending: 100, MaxBytes: 1 << 20, MaxAttempts: 8}}
	id, _, e := store.Accept(context.Background(), events.Work{Kind: "inbox", Connector: "reference", Tenant: "a", Type: "message", EventID: "evt_cli", Payload: []byte(`{}`)})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(action string, extra ...string) int {
		stdout.Reset()
		stderr.Reset()
		a := []string{"events", action, "--store", "queue", "--id", id, "--allow-db", "shop:read-write"}
		a = append(a, common...)
		a = append(a, extra...)
		return run(a, &stdout, &stderr)
	}
	if n := invoke("inspect"); n != 0 || !strings.Contains(stdout.String(), `"state":"pending"`) {
		t.Fatal(n, stdout.String(), stderr.String())
	}
	if n := invoke("retry", "--reason", "not dead"); n == 0 {
		t.Fatal("retried pending work")
	}
	work, e := store.Claim(context.Background(), "worker")
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Fail(context.Background(), work, false, false, 0); e != nil {
		t.Fatal(e)
	}
	if n := invoke("retry", "--reason", "corrected configuration"); n != 0 {
		t.Fatal(n, stderr.String())
	}
}
