package events

import "context"

// Metrics is an aggregate snapshot scoped to a configured namespace. It carries
// no work identifiers, tenants, payloads, SQL values or secret references.
type Metrics struct{ Pending, Dead, InboxLagMS, OutboxLagMS int64 }

func (s *Store) Metrics(ctx context.Context) (Metrics, error) {
	rows, e := s.DB.QueryContext(ctx, s.q("SELECT kind,state,COUNT(*),MIN(next_at) FROM grab_events_work WHERE namespace=? GROUP BY kind,state"), s.Config.Namespace)
	if e != nil {
		return Metrics{}, ErrStore
	}
	defer rows.Close()
	m := Metrics{}
	for rows.Next() {
		var kind, state string
		var count, first int64
		if rows.Scan(&kind, &state, &count, &first) != nil {
			return Metrics{}, ErrStore
		}
		if state == "dead" {
			m.Dead += count
		}
		if state == "pending" || state == "retry_wait" || state == "leased" {
			m.Pending += count
			lag := s.now() - first
			if lag < 0 {
				lag = 0
			}
			if kind == "inbox" && lag > m.InboxLagMS {
				m.InboxLagMS = lag
			}
			if kind == "outbox" && lag > m.OutboxLagMS {
				m.OutboxLagMS = lag
			}
		}
	}
	return m, rows.Err()
}
