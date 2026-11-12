package events

import (
	"context"
	"testing"
)

func TestMetricsAndLeaseRecovery(t *testing.T) {
	s, now := storeFixture(t)
	ctx := context.Background()
	if _, _, e := s.Accept(ctx, inbox()); e != nil {
		t.Fatal(e)
	}
	*now += 1000
	m, e := s.Metrics(ctx)
	if e != nil || m.Pending != 1 || m.InboxLagMS != 1000 || m.OutboxLagMS != 0 {
		t.Fatal(m, e)
	}
	first := mustClaim(t, s, "first")
	if first.Recovered {
		t.Fatal("new work marked recovered")
	}
	*now += 30001
	second := mustClaim(t, s, "second")
	if !second.Recovered || second.Token <= first.Token {
		t.Fatal("expired lease not counted", second)
	}
	if e = s.Fail(ctx, second, false, false, 0); e != nil {
		t.Fatal(e)
	}
	m, e = s.Metrics(ctx)
	if e != nil || m.Dead != 1 || m.Pending != 0 {
		t.Fatal(m, e)
	}
}
