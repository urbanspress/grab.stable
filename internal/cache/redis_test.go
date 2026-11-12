package cache

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"grab/internal/platform"
)

func TestRedisAddressPolicy(t *testing.T) {
	for _, tt := range []struct {
		ip      string
		def     platform.Cache
		allowed bool
	}{
		{"127.0.0.1", platform.Cache{}, false},
		{"127.0.0.1", platform.Cache{Host: "127.0.0.1", Development: true}, true},
		{"127.0.0.1", platform.Cache{Host: "localhost", Development: true}, false},
		{"10.0.0.5", platform.Cache{}, false},
		{"10.0.0.5", platform.Cache{AllowedCIDRs: []string{"10.0.0.0/24"}}, true},
		{"169.254.169.254", platform.Cache{AllowedCIDRs: []string{"0.0.0.0/0"}}, false},
		{"::", platform.Cache{}, false},
		{"ff02::1", platform.Cache{}, false},
		{"fe80::1", platform.Cache{AllowedCIDRs: []string{"::/0"}}, false},
		{"8.8.8.8", platform.Cache{}, true},
	} {
		if got := permittedIP(net.ParseIP(tt.ip), tt.def); got != tt.allowed {
			t.Fatal(tt.ip, got)
		}
	}
}
func TestRedisErrorClassification(t *testing.T) {
	for _, tt := range []struct {
		err   error
		write bool
		want  error
	}{
		{io.EOF, true, ErrUncertain}, {io.EOF, false, ErrUnavailable}, {context.DeadlineExceeded, true, ErrUncertain}, {errors.New("GRAB_VALUE private"), false, ErrValue}, {errors.New("GRAB_FULL private"), true, ErrFull},
	} {
		if got := redisError(tt.err, tt.write); !errors.Is(got, tt.want) {
			t.Fatal(got)
		}
	}
}
func TestRedisCapacityAndExactSaturation(t *testing.T) {
	port := os.Getenv("GRAB_TEST_REDIS_PORT")
	if port == "" {
		t.Skip("real Redis acceptance requires GRAB_TEST_REDIS_PORT")
	}
	namespace := "capacity-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	m, h := manager(t, `{"driver":"redis","namespace":"`+namespace+`","host":"127.0.0.1","port":`+port+`,"tls":"disabled","development":true,"max_keys":1,"max_bytes":1024}`, []string{"test:read-write", "test:limit"}, nil)
	ctx := context.Background()
	if _, e := m.Limit(ctx, h, "one", 1, 60000); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Limit(ctx, h, "two", 1, 60000); !errors.Is(e, ErrFull) {
		t.Fatal("evicted counter", e)
	}
	s := h.store.remote
	k := s.prefix + "limit:" + base64.RawURLEncoding.EncodeToString([]byte("one"))
	if e := s.client.HSet(ctx, k, "n", strconv.FormatInt(math.MaxInt64-1, 10)).Err(); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e := m.Limit(ctx, h, "one", 1, 60000)
		if e != nil || r.Count != math.MaxInt64 || r.Allowed {
			t.Fatal("inexact counter", r, e)
		}
	}
	// Expire naturally; stale quota metadata must not prevent a new admission.
	if e := s.client.PExpire(ctx, k, time.Millisecond).Err(); e != nil {
		t.Fatal(e)
	}
	if e := s.client.ZAdd(ctx, s.prefix+"index", redis.Z{Score: float64(time.Now().UnixMilli() + 1), Member: k}).Err(); e != nil {
		t.Fatal(e)
	}
	time.Sleep(10 * time.Millisecond)
	if _, e := m.Limit(ctx, h, "two", 1, 1000); e != nil {
		t.Fatal("expired key not reclaimed", e)
	}
}
