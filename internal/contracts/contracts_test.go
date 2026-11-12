package contracts

import (
	"bytes"
	"encoding/json"
	rt "grab/internal/runtime"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) *Document {
	t.Helper()
	b, e := os.ReadFile("../../tests/fixtures/typed-client/grab.api.json")
	if e != nil {
		t.Fatal(e)
	}
	d, e := Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestExactWire(t *testing.T) {
	d := fixture(t)
	s := Schema{Type: "ref", Ref: "Product"}
	for _, tc := range []struct {
		name, wire string
		valid      bool
	}{
		{"exact", `{"id":"90071992547409931234567890","price":"19.90","name":"Book","note":null,"bytes":"AAE="}`, true},
		{"number", `{"id":9007199254740993,"price":"19.90","name":"Book","bytes":""}`, false},
		{"duplicate", `{"id":"1","id":"2","price":"19.90","name":"Book","bytes":""}`, false},
		{"unknown", `{"id":"1","price":"19.90","name":"Book","bytes":"","other":true}`, false},
		{"scale", `{"id":"1","price":"19.9","name":"Book","bytes":""}`, false},
		{"leading zero", `{"id":"01","price":"19.90","name":"Book","bytes":""}`, false},
		{"bad base64", `{"id":"1","price":"19.90","name":"Book","bytes":"AB=="}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, e := d.Decode(s, []byte(tc.wire))
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, e)
			}
			if tc.valid {
				b, e := d.Encode(s, v)
				if e != nil || string(b) != tc.wire {
					t.Fatalf("roundtrip %s %v", b, e)
				}
			}
		})
	}
	big, _ := rt.IntegerFromString("90071992547409931234567890")
	b, e := d.Encode(Schema{Type: "integer"}, big)
	if e != nil || string(b) != `"90071992547409931234567890"` {
		t.Fatal(string(b), e)
	}
}
func TestInvalidContracts(t *testing.T) {
	for _, wire := range []string{
		`{"version":1,"types":{"A":{"type":"ref","ref":"A"}},"operations":[]}`,
		`{"version":1,"types":{"A":{"type":"text"}},"operations":[]}`,
		`{"version":1,"types":{},"operations":[],"secret":"x"}`,
		`{"version":1,"version":1,"types":{},"operations":[]}`,
		`{"version":1,"types":{"A":{"type":"integer","items":{"type":"boolean"}}},"operations":[]}`,
	} {
		if _, e := Parse([]byte(wire)); e == nil {
			t.Fatal(wire)
		}
	}
}
func TestGenerationOwnership(t *testing.T) {
	d := fixture(t)
	out := filepath.Join(t.TempDir(), "generated")
	if e := d.Generate(out, true); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(out, "client.ts"))
	if e := d.Generate(out, true); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(filepath.Join(out, "client.ts"))
	if !bytes.Equal(before, after) {
		t.Fatal("nondeterministic")
	}
	var public map[string]any
	b, _ := os.ReadFile(filepath.Join(out, "contract.json"))
	if json.Unmarshal(b, &public) != nil || public["hash"] != d.Hash() {
		t.Fatal("public contract")
	}
	os.WriteFile(filepath.Join(out, "client.ts"), []byte("handwritten"), 0600)
	if d.Generate(out, true) == nil {
		t.Fatal("overwrote edited file")
	}
	os.WriteFile(filepath.Join(out, "client.ts"), before, 0600)
	if e := d.Generate(out, false); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(out, "react.ts")); !os.IsNotExist(e) {
		t.Fatal("stale owned file")
	}
}
func TestParameters(t *testing.T) {
	d := fixture(t)
	o, _ := d.Operation("product")
	params := rt.Record([]rt.Entry{{Name: "id", Value: rt.Text("9007199254740993")}})
	query := rt.Map([]rt.Entry{{Name: "search", Value: rt.List([]rt.Value{rt.Text("x"), rt.Text("y")})}})
	req := rt.Record([]rt.Entry{{Name: "params", Value: params}, {Name: "query", Value: query}})
	if _, e := d.Input(o, req); e == nil {
		t.Fatal("duplicate scalar accepted")
	}
}
