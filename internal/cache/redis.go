package cache

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
	"grab/internal/platform"
)

type redisStore struct {
	client         *redis.Client
	prefix, policy string
}

func permittedIP(ip net.IP, d platform.Cache) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if v := ip.To4(); v != nil && (v[0] == 0 || v[0] >= 224) {
		return false
	}
	if ip.IsPrivate() || ip.IsLoopback() || !ip.IsGlobalUnicast() {
		if d.Development && ip.IsLoopback() && net.ParseIP(d.Host) != nil {
			return true
		}
		for _, s := range d.AllowedCIDRs {
			_, n, e := net.ParseCIDR(s)
			if e == nil && n.Contains(ip) {
				return true
			}
		}
		return false
	}
	return true
}
func newRedis(ctx context.Context, d platform.Cache, alias string) (*redisStore, error) {
	c, e := NewRedisClient(d)
	if e != nil {
		return nil, e
	}
	timeout := time.Duration(d.TimeoutMS) * time.Millisecond
	op, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if e = c.Ping(op).Err(); e != nil {
		_ = c.Close()
		return nil, ErrUnavailable
	}
	// Quotas and live counters depend on a dedicated non-evicting store. Never
	// change server settings; require the operator to configure them explicitly.
	policy, e := c.ConfigGet(op, "maxmemory-policy").Result()
	if e != nil || policy["maxmemory-policy"] != "noeviction" {
		_ = c.Close()
		return nil, ErrUnavailable
	}
	return &redisStore{c, "grab:v1:" + d.Namespace + ":" + alias + ":", strconv.Itoa(d.MaxKeys) + ":" + strconv.Itoa(d.MaxBytes)}, nil
}

// NewRedisClient builds a standalone, non-retrying client whose only dialer
// pins the configured endpoint, applies SSRF address policy and verified TLS.
// It performs no network I/O; the caller owns readiness checks and Close.
func NewRedisClient(d platform.Cache) (*redis.Client, error) {
	user, e := platform.Secret(d.UsernameEnv)
	if e != nil {
		return nil, e
	}
	password, e := platform.Secret(d.PasswordEnv)
	if e != nil {
		return nil, e
	}
	timeout := time.Duration(d.TimeoutMS) * time.Millisecond
	endpoint := net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	opts := &redis.Options{Addr: endpoint, Protocol: 2, Username: user, Password: password, MaxRetries: -1,
		PoolSize: d.PoolSize, MaxActiveConns: d.PoolSize, PipelinePoolSize: -1, MinIdleConns: 0,
		DialTimeout: timeout, ReadTimeout: timeout, WriteTimeout: timeout, PoolTimeout: timeout, ContextTimeoutEnabled: true,
		DisableIdentity: true, MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled}}
	opts.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != endpoint {
			return nil, platform.ErrDenied
		}
		ips := []net.IP{net.ParseIP(d.Host)}
		if ips[0] == nil {
			resolved, e := net.DefaultResolver.LookupIPAddr(ctx, d.Host)
			if e != nil {
				return nil, ErrUnavailable
			}
			ips = nil
			for _, a := range resolved {
				ips = append(ips, a.IP)
			}
		}
		if len(ips) == 0 {
			return nil, ErrUnavailable
		}
		for _, ip := range ips {
			if !permittedIP(ip, d) {
				return nil, platform.ErrDenied
			}
		}
		conn, e := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", net.JoinHostPort(ips[0].String(), strconv.Itoa(d.Port)))
		if e != nil {
			return nil, ErrUnavailable
		}
		if d.TLS == "verify" {
			t := tls.Client(conn, &tls.Config{ServerName: d.Host, MinVersion: tls.VersionTLS12})
			if e = t.HandshakeContext(ctx); e != nil {
				_ = conn.Close()
				return nil, ErrUnavailable
			}
			return t, nil
		}
		return conn, nil
	}
	return redis.NewClient(opts), nil
}
func (s *redisStore) close() error { return s.client.Close() }

// Only this fixed host-owned script is exposed. A sorted expiry index and size
// ledger account for both value and counter keys atomically across processes.
// HINCRBY plus HGET preserves int64 exactly beyond Lua's double precision.
const storeScript = `
local op=ARGV[1]
local key=KEYS[1]
local index=KEYS[2]
local sizes=KEYS[3]
local meta=KEYS[4]
local policy=redis.call('HGET',meta,'policy')
if policy and policy~=ARGV[2] then return redis.error_reply('GRAB_VALUE') end
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local expired=redis.call('ZRANGEBYSCORE',index,'-inf',now)
local total=tonumber(redis.call('HGET',meta,'bytes') or '0')
for _,k in ipairs(expired) do
 total=total-tonumber(redis.call('HGET',sizes,k) or '0')
 redis.call('DEL',k)
 redis.call('HDEL',sizes,k)
 redis.call('ZREM',index,k)
end
redis.call('HSET',meta,'bytes',total,'policy',ARGV[2])
local live=redis.call('EXISTS',key)==1
if op=='get' then
 if not live then return {0,'',0} end
 local length=redis.call('HSTRLEN',key,'v')
 if length>1048576 then return redis.error_reply('GRAB_VALUE') end
 local value=redis.call('HGET',key,'v')
 if not value then return redis.error_reply('GRAB_VALUE') end
 local ttl=redis.call('PTTL',key)
 if ttl<1 then return redis.error_reply('GRAB_VALUE') end
 return {1,value,ttl}
end
if op=='delete' then
 local size=tonumber(redis.call('HGET',sizes,key) or '0')
 local removed=redis.call('DEL',key)
 redis.call('HDEL',sizes,key)
 redis.call('ZREM',index,key)
 redis.call('HSET',meta,'bytes',total-size)
 return removed
end
local maxkeys,maxbytes=string.match(ARGV[2],'^(%d+):(%d+)$')
local size=tonumber(ARGV[3])
local oldsize=tonumber(redis.call('HGET',sizes,key) or '0')
if (not live and redis.call('ZCARD',index)>=tonumber(maxkeys)) or total-oldsize+size>tonumber(maxbytes) then return redis.error_reply('GRAB_FULL') end
if op=='limit' and live then
 if redis.call('HGET',key,'m')~=ARGV[5] or redis.call('HGET',key,'w')~=ARGV[4] then return redis.error_reply('GRAB_VALUE') end
end
if op=='set' then
 redis.call('HSET',key,'v',ARGV[5])
 redis.call('PEXPIRE',key,ARGV[4])
 redis.call('ZADD',index,now+tonumber(ARGV[4]),key)
else
 if not live then
  redis.call('HSET',key,'n','0','m',ARGV[5],'w',ARGV[4])
  redis.call('PEXPIRE',key,ARGV[4])
  redis.call('ZADD',index,now+tonumber(ARGV[4]),key)
 end
 local count=redis.call('HGET',key,'n')
 if count~='9223372036854775807' then redis.call('HINCRBY',key,'n',1) end
end
redis.call('HSET',sizes,key,size)
redis.call('HSET',meta,'bytes',total-oldsize+size)
if op=='limit' then return {redis.call('HGET',key,'n'),redis.call('PTTL',key)} end
return 1
`

func redisError(e error, write bool) error {
	if e == nil {
		return nil
	}
	if strings.Contains(e.Error(), "GRAB_VALUE") {
		return ErrValue
	}
	if strings.Contains(e.Error(), "GRAB_FULL") || strings.HasPrefix(e.Error(), "OOM ") {
		return ErrFull
	}
	var server redis.Error
	if errors.As(e, &server) {
		return ErrUnavailable
	}
	if write {
		return ErrUncertain
	}
	return ErrUnavailable
}
func (s *redisStore) call(ctx context.Context, op, key string, size int, ttl int64, value any) (any, error) {
	space := "value:"
	if op == "limit" {
		space = "limit:"
	}
	k := s.prefix + space + base64.RawURLEncoding.EncodeToString([]byte(key))
	v, e := s.client.Eval(ctx, storeScript, []string{k, s.prefix + "index", s.prefix + "sizes", s.prefix + "meta"}, op, s.policy, size, ttl, value).Result()
	return v, redisError(e, op != "get")
}
func (s *redisStore) get(ctx context.Context, key string) (Result, error) {
	v, e := s.call(ctx, "get", key, 0, 0, "")
	if e != nil {
		return Result{}, e
	}
	a, ok := v.([]any)
	if !ok || len(a) != 3 {
		return Result{}, ErrValue
	}
	found, ok := a[0].(int64)
	if !ok {
		return Result{}, ErrValue
	}
	if found == 0 {
		return Result{}, nil
	}
	raw, ok := a[1].(string)
	if !ok {
		return Result{}, ErrValue
	}
	ttl, ok := a[2].(int64)
	if !ok || ttl < 1 {
		return Result{}, ErrValue
	}
	x, e := Decode([]byte(raw))
	if e != nil {
		return Result{}, ErrValue
	}
	return Result{true, x, ttl}, nil
}
func (s *redisStore) set(ctx context.Context, key string, raw []byte, ttl int64) error {
	_, e := s.call(ctx, "set", key, len(key)+len(raw)+128, ttl, string(raw))
	return e
}
func (s *redisStore) delete(ctx context.Context, key string) (bool, error) {
	v, e := s.call(ctx, "delete", key, 0, 0, "")
	n, ok := v.(int64)
	if e == nil && !ok {
		return false, ErrValue
	}
	return n != 0, e
}
func (s *redisStore) limit(ctx context.Context, key string, maximum, window int64) (LimitResult, error) {
	v, e := s.call(ctx, "limit", key, len(key)+128, window, strconv.FormatInt(maximum, 10))
	if e != nil {
		return LimitResult{}, e
	}
	a, ok := v.([]any)
	if !ok || len(a) != 2 {
		return LimitResult{}, ErrValue
	}
	text, ok := a[0].(string)
	if !ok {
		return LimitResult{}, ErrValue
	}
	count, e := strconv.ParseInt(text, 10, 64)
	if e != nil || count < 1 {
		return LimitResult{}, ErrValue
	}
	ttl, ok := a[1].(int64)
	if !ok || ttl < 0 {
		return LimitResult{}, ErrValue
	}
	return limitResult(count, maximum, ttl), nil
}
