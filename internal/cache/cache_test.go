package cache

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"grab/internal/platform"
	rt "grab/internal/runtime"
)

func manager(t *testing.T, def string, grants []string, now func() time.Time) (*Manager, *Handle) {
	t.Helper()
	c, e := platform.Parse([]byte(`{"version":1,"caches":{"test":`+def+`}}`), filepath.Join(t.TempDir(), "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	g, e := c.Grant(map[string][]string{"cache": grants})
	if e != nil {
		t.Fatal(e)
	}
	m := NewManager(g, now)
	t.Cleanup(func() { _ = m.Close() })
	h, e := m.Open(context.Background(), "test")
	if e != nil {
		t.Fatal(e)
	}
	return m, h
}
func codecValues(t *testing.T) []rt.Value {
	t.Helper()
	integer, _ := rt.IntegerFromString("9999999999999999999999999999999999999999999")
	decimal, _ := rt.DecimalFromString("123456789012345678901234567890.00100")
	float, _ := rt.FloatFromString("-0.12345678901234567")
	return []rt.Value{rt.Nothing(), rt.Boolean(true), rt.Boolean(false), rt.Text("héllo\n世界"), integer, decimal, float, rt.Bytes([]byte{0, 1, 254, 255}), rt.List([]rt.Value{integer, decimal, rt.Nothing()}), rt.Map([]rt.Entry{{Name: "z", Value: decimal}, {Name: "a", Value: integer}}), rt.Record([]rt.Entry{{Name: "price", Value: decimal}, {Name: "optional", Value: rt.Nothing()}})}
}
func TestCodec(t *testing.T) {
	if _, e := Encode(rt.List(make([]rt.Value, 10000))); e != nil {
		t.Fatal("valid 10,000-element boundary rejected", e)
	}
	for _, v := range codecValues(t) {
		raw, e := Encode(v)
		if e != nil {
			t.Fatal(e)
		}
		got, e := Decode(raw)
		if e != nil || got.Kind() != v.Kind() || !got.Equal(v) || got.Display() != v.Display() {
			t.Fatalf("kind=%v err=%v", v.Kind(), e)
		}
		again, _ := Encode(got)
		if string(again) != string(raw) {
			t.Fatal("not canonical")
		}
	}
	for _, raw := range []string{`{"version":2,"value":{"kind":0}}`, `{"version":1,"value":{"kind":0,"scalar":"secret"}}`, `{"version":1,"value":{"kind":0,"kind":1}}`, `{"version":1,"value":{"kind":3,"scalar":"001"}}`, `{"version":1,"value":{"kind":5,"scalar":"NaN"}}`, `{"version":1,"value":{"kind":10}}`, `{"version":1,"value":{"kind":0}} {}`, `{"version":1,"value":{"kind":4,"scalar":"1","scale":2147483647}}`} {
		if _, e := Decode([]byte(raw)); e == nil {
			t.Fatal("accepted corrupt encoding", raw)
		}
	}
	for _, v := range []rt.Value{rt.Resource("opaque"), rt.List([]rt.Value{rt.Resource("opaque")}), rt.Text(strings.Repeat("x", MaxValueBytes)), rt.Text(string([]byte{255})), rt.Map([]rt.Entry{{Name: "x"}, {Name: "x"}}), rt.List(make([]rt.Value, 10001))} {
		if _, e := Encode(v); e == nil {
			t.Fatal("accepted invalid value")
		}
	}
	v := rt.Nothing()
	for i := 0; i < 33; i++ {
		v = rt.List([]rt.Value{v})
	}
	if _, e := Encode(v); e == nil {
		t.Fatal("depth not bounded")
	}
}

// Shared vectors are intentionally executed against both real Redis and memory.
func cacheVectors(t *testing.T, m *Manager, h *Handle) {
	t.Helper()
	ctx := context.Background()
	for i, v := range codecValues(t) {
		key := string(rune('a' + i))
		if e := m.Set(ctx, h, key, v, 60000); e != nil {
			t.Fatal(e)
		}
		r, e := m.Get(ctx, h, key)
		if e != nil || !r.Found || r.TTLMS < 1 || r.TTLMS > 60000 || r.Value.Display() != v.Display() || r.Value.Kind() != v.Kind() {
			t.Fatalf("roundtrip %d: %+v %v", i, r, e)
		}
		if yes, e := m.Delete(ctx, h, key); e != nil || !yes {
			t.Fatal("delete", e)
		}
		if yes, e := m.Delete(ctx, h, key); e != nil || yes {
			t.Fatal("duplicate delete", e)
		}
	}
	if r, e := m.Get(ctx, h, "absent"); e != nil || r.Found || r.TTLMS != 0 || r.Value.Kind() != rt.NothingKind {
		t.Fatal("miss", r, e)
	}
	for i := int64(1); i <= 4; i++ {
		r, e := m.Limit(ctx, h, "person", 2, 60000)
		if e != nil || r.Count != i || r.Allowed != (i <= 2) || r.Remaining != max(int64(0), 2-i) || i > 2 && r.RetryAfterMS < 1 {
			t.Fatal(r, e)
		}
	}
	if _, e := m.Limit(ctx, h, "person", 3, 60000); !errors.Is(e, ErrValue) {
		t.Fatal("changed maximum", e)
	}
	if _, e := m.Limit(ctx, h, "person", 2, 30000); !errors.Is(e, ErrValue) {
		t.Fatal("changed window", e)
	}
	if e := m.Set(ctx, h, "person", rt.Text("ordinary"), 60000); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Delete(ctx, h, "person"); e != nil {
		t.Fatal(e)
	}
	if r, e := m.Limit(ctx, h, "person", 2, 60000); e != nil || r.Count != 5 {
		t.Fatal("namespace collision", r, e)
	}
	for _, key := range []string{"", "\n", "a\x00b", strings.Repeat("a", 257), string([]byte{255})} {
		if e := m.Set(ctx, h, key, rt.Nothing(), 1); e == nil {
			t.Fatal("bad key")
		}
	}
	for _, ttl := range []int64{-1, 0, MaxTTL + 1} {
		if e := m.Set(ctx, h, "ttl", rt.Nothing(), ttl); !errors.Is(e, ErrValue) {
			t.Fatal("ttl", e)
		}
	}
	for _, args := range [][2]int64{{0, 1}, {1000001, 1}, {1, 0}, {1, 86400001}} {
		if _, e := m.Limit(ctx, h, "bounds", args[0], args[1]); !errors.Is(e, ErrValue) {
			t.Fatal("limit bound", e)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := m.Get(cancelled, h, "absent"); !errors.Is(e, context.Canceled) {
		t.Fatal("cancel", e)
	}
	var wg sync.WaitGroup
	counts := make(chan int64, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := m.Limit(ctx, h, "parallel", 50, 60000)
			if e != nil {
				t.Error(e)
				return
			}
			counts <- r.Count
		}()
	}
	wg.Wait()
	close(counts)
	seen := map[int64]bool{}
	for n := range counts {
		if seen[n] {
			t.Fatal("duplicate count")
		}
		seen[n] = true
	}
	if len(seen) != 100 {
		t.Fatal("lost increment")
	}
}
func TestMemoryVectors(t *testing.T) {
	m, h := manager(t, `{"driver":"memory","namespace":"vectors"}`, []string{"test:read-write", "test:limit"}, nil)
	cacheVectors(t, m, h)
}
func TestMemoryClockCapacityAndSaturation(t *testing.T) {
	now := time.Now()
	m, h := manager(t, `{"driver":"memory","namespace":"clock","max_keys":1,"max_bytes":1024}`, []string{"test:read-write", "test:limit"}, func() time.Time { return now })
	ctx := context.Background()
	r, e := m.Limit(ctx, h, "one", 1, 100)
	if e != nil || r.Count != 1 {
		t.Fatal(r, e)
	}
	now = now.Add(40 * time.Millisecond)
	r, e = m.Limit(ctx, h, "one", 1, 100)
	if e != nil || r.RetryAfterMS != 60 {
		t.Fatal(r, e)
	}
	if e = m.Set(ctx, h, "two", rt.Nothing(), 1); !errors.Is(e, ErrFull) {
		t.Fatal("evicted live counter", e)
	}
	v := h.store.entries["limit:one"]
	v.count = math.MaxInt64
	h.store.entries["limit:one"] = v
	r, e = m.Limit(ctx, h, "one", 1, 100)
	if e != nil || r.Count != math.MaxInt64 || r.Allowed {
		t.Fatal("overflow", r, e)
	}
	now = now.Add(60 * time.Millisecond)
	if e = m.Set(ctx, h, "two", rt.Nothing(), 10); e != nil {
		t.Fatal(e)
	}
	if r, e := m.Get(ctx, h, "two"); e != nil || !r.Found || r.Value.Kind() != rt.NothingKind {
		t.Fatal("stored nothing", r, e)
	}
	now = now.Add(10 * time.Millisecond)
	if r, e := m.Get(ctx, h, "two"); e != nil || r.Found {
		t.Fatal("expiry", r, e)
	}
	if e = m.Set(ctx, h, "too-big", rt.Text(strings.Repeat("x", 1000)), 100); !errors.Is(e, ErrFull) {
		t.Fatal("bytes unbounded", e)
	}
}
func TestOwnershipPermissionsClose(t *testing.T) {
	ctx := context.Background()
	m, h := manager(t, `{"driver":"memory","namespace":"owner"}`, []string{"test:read"}, nil)
	if e := m.Set(ctx, h, "x", rt.Nothing(), 1); !Denied(e) {
		t.Fatal(e)
	}
	if _, e := m.Limit(ctx, h, "x", 1, 1); !Denied(e) {
		t.Fatal(e)
	}
	other := NewManager(nil, nil)
	defer other.Close()
	if _, e := other.Get(ctx, h, "x"); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	_ = m.Close()
	_ = m.Close()
	if _, e := m.Get(ctx, h, "x"); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
	if _, e := m.Open(ctx, "test"); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
}
func TestRedisVectors(t *testing.T) {
	port := os.Getenv("GRAB_TEST_REDIS_PORT")
	if port == "" {
		t.Skip("real Redis acceptance requires GRAB_TEST_REDIS_PORT")
	}
	namespace := "test-" + time.Now().Format("150405000000000")
	m, h := manager(t, `{"driver":"redis","namespace":"`+namespace+`","host":"127.0.0.1","port":`+port+`,"tls":"disabled","development":true}`, []string{"test:read-write", "test:limit"}, nil)
	cacheVectors(t, m, h)
	ctx := context.Background()
	if e := m.Set(ctx, h, "short", rt.Text("x"), 20); e != nil {
		t.Fatal(e)
	}
	time.Sleep(40 * time.Millisecond)
	if r, e := m.Get(ctx, h, "short"); e != nil || r.Found {
		t.Fatal("real expiry", r, e)
	}
}
