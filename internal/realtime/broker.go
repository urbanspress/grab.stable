// Package realtime owns transient, bounded channel transport, never language state.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
	"grab/internal/cache"
	"grab/internal/platform"
)

var ErrUnavailable = errors.New("GRAB-E318: channel unavailable")
var ErrPolicy = errors.New("GRAB-E318: channel policy rejected")

// Audience identifiers are opaque host-created IDs, not Redis patterns or paths.
func Audience(s string) bool {
	if len(s) < 1 || len(s) > 128 || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == ':') {
			return false
		}
	}
	return true
}

type publication struct {
	Audience string          `json:"audience"`
	Value    json.RawMessage `json:"value"`
}
type Broker struct {
	mu      sync.Mutex
	closed  bool
	clients map[*connection]bool
	redis   *redis.Client
	sub     *redis.PubSub
	topic   string
	cancel  context.CancelFunc
	done    chan struct{}
}

func (b *Broker) Ready() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.closed }
func (b *Broker) PoolUse() int64 {
	if b.redis == nil {
		return 0
	}
	s := b.redis.PoolStats()
	return int64(s.TotalConns-s.IdleConns) + 1 // dedicated subscription
}

func NewBroker(ctx context.Context, alias string, d platform.ChannelBroker) (*Broker, error) {
	b := &Broker{clients: map[*connection]bool{}, done: make(chan struct{})}
	if d.Driver == "memory" {
		close(b.done)
		return b, nil
	}
	c, e := cache.NewRedisClient(d.RedisCache())
	if e != nil {
		return nil, ErrUnavailable
	}
	b.redis = c
	b.topic = "grab:channels:v1:" + d.Namespace + ":" + alias
	lifetime, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.sub = c.Subscribe(lifetime, b.topic)
	if _, e = b.sub.Receive(ctx); e != nil {
		close(b.done)
		b.Close()
		return nil, ErrUnavailable
	}
	go func() {
		defer close(b.done)
		for {
			msg, e := b.sub.ReceiveMessage(lifetime)
			if e != nil {
				b.fail()
				return
			}
			if len(msg.Payload) > 2<<20 {
				b.fail()
				return
			}
			var p publication
			if platform.StrictValueJSON([]byte(msg.Payload), &p) != nil || !Audience(p.Audience) {
				b.fail()
				return
			}
			b.deliver(p.Audience, []byte(msg.Payload))
		}
	}()
	return b, nil
}
func (b *Broker) fail() {
	b.mu.Lock()
	b.closed = true
	for c := range b.clients {
		c.stop(1013)
	}
	b.mu.Unlock()
}
func (b *Broker) Publish(ctx context.Context, audience string, value []byte) (bool, error) {
	if !Audience(audience) {
		return false, ErrPolicy
	}
	raw, e := json.Marshal(publication{audience, value})
	if e != nil {
		return false, ErrPolicy
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return false, ErrUnavailable
	}
	if b.redis != nil {
		if b.redis.Publish(ctx, b.topic, raw).Err() != nil {
			b.fail()
			return false, ErrUnavailable
		}
		return true, nil
	}
	b.deliver(audience, raw)
	return true, nil
}
func (b *Broker) deliver(audience string, raw []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.clients {
		if c.audiences[audience] {
			c.enqueue(raw)
		}
	}
}
func (b *Broker) Close() {
	b.mu.Lock()
	b.closed = true
	for c := range b.clients {
		c.stop(1012)
	}
	b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
	if b.sub != nil {
		_ = b.sub.Close()
	}
	if b.redis != nil {
		_ = b.redis.Close()
	}
	<-b.done
}
