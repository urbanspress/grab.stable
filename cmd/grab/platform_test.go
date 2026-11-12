package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformCLI(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "grab.platform.json")
	program := filepath.Join(dir, "app.grab")
	if e := os.WriteFile(config, []byte(`{"version":1,"caches":{"shop":{"driver":"memory","namespace":"cli"}}}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(program, []byte("use cache\nset store to cache.open using \"shop\"\ncall cache.set using store, \"key\", 12.50, 1000\nset hit to cache.get using store, \"key\"\nshow hit.value\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		name      string
		args      []string
		exit      int
		out, code string
	}{
		{"run", []string{"run", "--platform-config", config, "--allow-cache", "shop:read-write", program}, 0, "12.50\n", ""},
		{"check without grants", []string{"check", "--platform-config", config, program}, 0, "", ""},
		{"deny by default", []string{"run", "--platform-config", config, program}, 1, "", "GRAB-E403"},
		{"read does not write", []string{"run", "--platform-config", config, "--allow-cache", "shop:read", program}, 1, "", "GRAB-E403"},
		{"wildcard", []string{"run", "--platform-config", config, "--allow-cache", "*:read-write", program}, 1, "", "GRAB-E403"},
		{"grant without config", []string{"run", "--allow-cache", "shop:read", program}, 1, "", "GRAB-E403"},
		{"missing config", []string{"check", "--platform-config", filepath.Join(dir, "missing.json"), program}, 1, "", "GRAB-E210"},
		{"duplicate config", []string{"check", "--platform-config", config, "--platform-config", config, program}, 2, "", "usage:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, err bytes.Buffer
			code := run(tt.args, &out, &err)
			if code != tt.exit || out.String() != tt.out || !strings.Contains(err.String(), tt.code) {
				t.Fatalf("exit=%d out=%q err=%q", code, out.String(), err.String())
			}
		})
	}
}
func TestPlatformCheckDoesNotResolveSecretsOrDial(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "grab.platform.json")
	program := filepath.Join(dir, "app.grab")
	t.Setenv("GRAB_TEST_UNRESOLVED_SECRET", "")
	if e := os.WriteFile(config, []byte(`{"version":1,"caches":{"shop":{"driver":"redis","namespace":"offline","host":"unreachable.invalid","port":6379,"tls":"verify","password_env":"GRAB_TEST_UNRESOLVED_SECRET"}}}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(program, []byte("use cache\nset store to cache.open using \"shop\"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var out, err bytes.Buffer
	if code := run([]string{"check", "--platform-config", config, program}, &out, &err); code != 0 {
		t.Fatal(code, err.String())
	}
}
