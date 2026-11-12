package parser

import "testing"

func TestCacheSetContextualMember(t *testing.T) {
	for _, tt := range []struct {
		source string
		valid  bool
	}{
		{"call cache.set using store, \"key\", 1, 100\n", true},
		{"set result to cache.set using store, \"key\", 1, 100\n", true},
		{"show call cache.set using store, \"key\", 1, 100\n", true},
		{"call other.set using 1\n", false},
		{"set set to 1\n", false},
		{"call cache.show using 1\n", false},
	} {
		p, e := Parse("cache.grab", tt.source)
		if (e == nil) != tt.valid {
			t.Fatalf("%q: %v", tt.source, e)
		}
		if p != nil {
			formatted, e := Format("cache.grab", tt.source)
			if e != nil {
				t.Fatal(e)
			}
			again, e := Format("again.grab", formatted)
			if e != nil || again != formatted {
				t.Fatal("unstable formatting", e)
			}
		}
	}
}
