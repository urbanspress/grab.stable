// Package events owns SQL-backed inbox/outbox state. Redis and memory queues
// are deliberately not durable event stores. All SQL is host-defined.
package events

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"grab/internal/data"
	"grab/internal/platform"
	"strconv"
	"strings"
	"time"
)

var ErrStore = errors.New("GRAB-E317: durable event operation failed")
var ErrUncertain = errors.New("GRAB-E317: durable commit outcome uncertain")
var ErrConflict = errors.New("GRAB-E317: event identity has conflicting content")
var ErrFull = errors.New("GRAB-E317: durable queue full")
var ErrFence = errors.New("GRAB-E317: stale event lease")

type Store struct {
	DB     *sql.DB
	Driver string
	Config platform.EventStore
	Now    func() time.Time
}
type Work struct {
	Recovered      bool
	ID             string
	Kind           string
	Connector      string
	Tenant         string
	EventID        string
	Type           string
	Payload        []byte
	Digest         string
	Operation      string
	Key            string
	State          string
	Attempts       int
	Next           int64
	Deadline       int64
	Owner          string
	Token          int64
	Lease          int64
	Bytes          int
	Resolved       string
	Classification string
}
type Status struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Attempts int    `json:"attempts"`
	Next     int64  `json:"next_attempt_at"`
}

func ID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("host random source unavailable")
	}
	return hex.EncodeToString(b[:])
}
func (s *Store) now() int64 {
	if s.Now != nil {
		return s.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}
func (s *Store) q(q string) string {
	if s.Driver != "postgres" {
		return q
	}
	i := 0
	var b strings.Builder
	for _, c := range q {
		if c == '?' {
			i++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(i))
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) exec(ctx context.Context, x executor, q string, a ...any) (sql.Result, error) {
	return x.ExecContext(ctx, s.q(q), a...)
}
func (s *Store) row(ctx context.Context, x executor, q string, a ...any) *sql.Row {
	return x.QueryRowContext(ctx, s.q(q), a...)
}

// MigrationSteps are published and applied explicitly through grab migrate.
// One budget row serializes admission and claims within each namespace.
func MigrationSteps(driver string) []string {
	text := "TEXT"
	blob := "BLOB"
	if driver == "postgres" {
		blob = "BYTEA"
	}
	if driver == "mariadb" {
		text = "VARCHAR(255)"
		blob = "MEDIUMBLOB"
	}
	return []string{
		"CREATE TABLE grab_events_budget (namespace " + text + " PRIMARY KEY, pending BIGINT NOT NULL, payload_bytes BIGINT NOT NULL)",
		"CREATE TABLE grab_events_work (namespace " + text + " NOT NULL, id VARCHAR(64) NOT NULL, kind VARCHAR(16) NOT NULL, connector VARCHAR(64) NOT NULL, tenant VARCHAR(64) NOT NULL, event_id VARCHAR(128) NOT NULL, event_type VARCHAR(64) NOT NULL, payload " + blob + " NOT NULL, digest VARCHAR(64) NOT NULL, operation VARCHAR(64) NOT NULL, idempotency_key VARCHAR(256) NOT NULL, state VARCHAR(32) NOT NULL, attempts INTEGER NOT NULL, next_at BIGINT NOT NULL, deadline BIGINT NOT NULL, owner VARCHAR(64) NOT NULL, token BIGINT NOT NULL, lease_until BIGINT NOT NULL, payload_bytes BIGINT NOT NULL, delivery_class VARCHAR(16) NOT NULL, resolved VARCHAR(32) NOT NULL, PRIMARY KEY(namespace,id), UNIQUE(namespace,kind,connector,tenant,event_id))",
		"CREATE INDEX grab_events_claim ON grab_events_work(namespace,state,next_at)",
		"CREATE TABLE grab_events_audit (namespace " + text + " NOT NULL, id VARCHAR(64) NOT NULL, work_id VARCHAR(64) NOT NULL, action VARCHAR(32) NOT NULL, reason " + blob + " NOT NULL, occurred_at BIGINT NOT NULL, PRIMARY KEY(namespace,id))",
	}
}
func (s *Store) Ready(ctx context.Context) error {
	var v, dirty int
	var checksum string
	if s.DB.QueryRowContext(ctx, "SELECT version,dirty,checksum FROM grab_events_migrations ORDER BY version DESC").Scan(&v, &dirty, &checksum) != nil || v != 1 || dirty != 0 || checksum != data.EventMigrationChecksum(MigrationSteps(s.Driver)) {
		return ErrStore
	}
	var pending int
	if s.row(ctx, s.DB, "SELECT pending FROM grab_events_budget WHERE namespace=?", s.Config.Namespace).Scan(&pending) != nil {
		return ErrStore
	}
	return nil
}
func (s *Store) InitNamespace(ctx context.Context) error {
	// Explicit maintenance, after migration. Runtime never creates namespaces.
	_, e := s.exec(ctx, s.DB, "INSERT INTO grab_events_budget(namespace,pending,payload_bytes) VALUES(?,0,0)", s.Config.Namespace)
	if e != nil {
		var n int
		if s.row(ctx, s.DB, "SELECT pending FROM grab_events_budget WHERE namespace=?", s.Config.Namespace).Scan(&n) == nil {
			return nil
		}
		return ErrStore
	}
	return nil
}
func (s *Store) lock(ctx context.Context, tx *sql.Tx) error {
	r, e := s.exec(ctx, tx, "UPDATE grab_events_budget SET pending=pending WHERE namespace=?", s.Config.Namespace)
	if e != nil {
		return ErrStore
	}
	n, e := r.RowsAffected()
	if e != nil {
		return ErrStore
	}
	if n == 0 {
		var x int
		if s.row(ctx, tx, "SELECT pending FROM grab_events_budget WHERE namespace=?", s.Config.Namespace).Scan(&x) != nil {
			return ErrStore
		}
	}
	return nil
}
func (s *Store) begin(ctx context.Context) (*sql.Tx, error) {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return nil, ErrStore
	}
	if e = s.lock(ctx, tx); e != nil {
		tx.Rollback()
		return nil, e
	}
	return tx, nil
}
func commit(tx *sql.Tx) error {
	if e := tx.Commit(); e != nil {
		return ErrUncertain
	}
	return nil
}
func (s *Store) admit(ctx context.Context, tx *sql.Tx, size int) error {
	r, e := s.exec(ctx, tx, "UPDATE grab_events_budget SET pending=pending+1,payload_bytes=payload_bytes+? WHERE namespace=? AND pending<? AND payload_bytes<=?", size, s.Config.Namespace, s.Config.MaxPending, s.Config.MaxBytes-size)
	if e != nil {
		return ErrStore
	}
	n, e := r.RowsAffected()
	if e != nil || n != 1 {
		return ErrFull
	}
	return nil
}
func (s *Store) insert(ctx context.Context, tx *sql.Tx, w Work) error {
	if len(w.Payload) > 1<<20 || len(w.Key) > 256 || len(w.ID) > 64 {
		return ErrStore
	}
	if e := s.admit(ctx, tx, len(w.Payload)); e != nil {
		return e
	}
	now := s.now()
	h := sha256.Sum256(w.Payload)
	if w.Digest == "" {
		w.Digest = hex.EncodeToString(h[:])
	}
	_, e := s.exec(ctx, tx, "INSERT INTO grab_events_work(namespace,id,kind,connector,tenant,event_id,event_type,payload,digest,operation,idempotency_key,state,attempts,next_at,deadline,owner,token,lease_until,payload_bytes,delivery_class,resolved) VALUES(?,?,?,?,?,?,?,?,?,?,?,'pending',0,?,?,'',0,0,?,?,?)", s.Config.Namespace, w.ID, w.Kind, w.Connector, w.Tenant, w.EventID, w.Type, w.Payload, w.Digest, w.Operation, w.Key, now, now+int64(24*time.Hour/time.Millisecond), len(w.Payload), w.Classification, w.Resolved)
	if e != nil {
		return ErrStore
	}
	return nil
}
func (s *Store) Accept(ctx context.Context, w Work) (string, bool, error) {
	return s.AcceptRaw(ctx, w, w.Payload)
}
func (s *Store) AcceptRaw(ctx context.Context, w Work, raw []byte) (string, bool, error) {
	if w.Kind != "inbox" || w.EventID == "" {
		return "", false, ErrStore
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return "", false, e
	}
	defer tx.Rollback()
	var existing, digest string
	h := sha256.Sum256(raw)
	want := hex.EncodeToString(h[:])
	w.Digest = want
	e = s.row(ctx, tx, "SELECT id,digest FROM grab_events_work WHERE namespace=? AND kind='inbox' AND connector=? AND tenant=? AND event_id=?", s.Config.Namespace, w.Connector, w.Tenant, w.EventID).Scan(&existing, &digest)
	if e == nil {
		if want != digest {
			return "", false, ErrConflict
		}
		return existing, true, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", false, ErrStore
	}
	w.ID = ID()
	if e = s.insert(ctx, tx, w); e != nil {
		return "", false, e
	}
	if e = commit(tx); e != nil {
		return "", false, e
	}
	return w.ID, false, nil
}

// Enqueue must use the same transaction as business changes. It never commits.
func (s *Store) Enqueue(ctx context.Context, tx *sql.Tx, w Work) (string, error) {
	if w.Kind != "outbox" || w.Key == "" || len(w.Key) > 256 {
		return "", ErrStore
	}
	if e := s.lock(ctx, tx); e != nil {
		return "", e
	}
	w.ID = ID()
	w.EventID = w.ID
	if e := s.insert(ctx, tx, w); e != nil {
		return "", e
	}
	return w.ID, nil
}

const columns = "id,kind,connector,tenant,event_id,event_type,payload,digest,operation,idempotency_key,state,attempts,next_at,deadline,owner,token,lease_until,payload_bytes,delivery_class,resolved"

func scan(r *sql.Row) (Work, error) {
	var w Work
	e := r.Scan(&w.ID, &w.Kind, &w.Connector, &w.Tenant, &w.EventID, &w.Type, &w.Payload, &w.Digest, &w.Operation, &w.Key, &w.State, &w.Attempts, &w.Next, &w.Deadline, &w.Owner, &w.Token, &w.Lease, &w.Bytes, &w.Classification, &w.Resolved)
	return w, e
}
func (s *Store) Claim(ctx context.Context, owner string) (Work, error) {
	if len(owner) < 1 || len(owner) > 64 {
		return Work{}, ErrStore
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return Work{}, e
	}
	defer tx.Rollback()
	now := s.now()
	w, e := scan(s.row(ctx, tx, "SELECT "+columns+" FROM grab_events_work WHERE namespace=? AND ((state IN ('pending','retry_wait') AND next_at<=?) OR (state='leased' AND lease_until<=?)) ORDER BY next_at,id LIMIT 1", s.Config.Namespace, now, now))
	if errors.Is(e, sql.ErrNoRows) {
		return Work{}, nil
	}
	if e != nil {
		return Work{}, ErrStore
	}
	if w.Deadline <= now || w.Attempts >= s.Config.MaxAttempts {
		if e = s.terminal(ctx, tx, w, "dead"); e != nil {
			return Work{}, e
		}
		if e = commit(tx); e != nil {
			return Work{}, e
		}
		return Work{}, nil
	}
	// Reclaim after a send of unsafe work cannot distinguish success from loss.
	if w.State == "leased" && w.Kind == "outbox" && w.Classification == "unsafe" {
		if _, e = s.exec(ctx, tx, "UPDATE grab_events_work SET state='uncertain',owner='',lease_until=0 WHERE namespace=? AND id=?", s.Config.Namespace, w.ID); e != nil {
			return Work{}, ErrStore
		}
		return Work{}, commit(tx)
	}
	r, e := s.exec(ctx, tx, "UPDATE grab_events_work SET state='leased',owner=?,token=token+1,lease_until=?,attempts=attempts+1 WHERE namespace=? AND id=? AND token=? AND token<9223372036854775807", owner, now+30000, s.Config.Namespace, w.ID, w.Token)
	if e != nil {
		return Work{}, ErrStore
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return Work{}, ErrFence
	}
	if e = commit(tx); e != nil {
		return Work{}, e
	}
	w.Recovered = w.State == "leased"
	w.State = "leased"
	w.Owner = owner
	w.Token++
	w.Lease = now + 30000
	w.Attempts++
	return w, nil
}
func (s *Store) Fence(ctx context.Context, tx *sql.Tx, w Work) error {
	if e := s.lock(ctx, tx); e != nil {
		return e
	}
	r, e := s.exec(ctx, tx, "UPDATE grab_events_work SET lease_until=lease_until WHERE namespace=? AND id=? AND state='leased' AND owner=? AND token=? AND lease_until>?", s.Config.Namespace, w.ID, w.Owner, w.Token, s.now())
	if e != nil {
		return ErrStore
	}
	n, e := r.RowsAffected()
	if e != nil {
		return ErrStore
	}
	if n != 1 {
		var token int64
		if s.row(ctx, tx, "SELECT token FROM grab_events_work WHERE namespace=? AND id=? AND state='leased' AND owner=? AND token=? AND lease_until>?", s.Config.Namespace, w.ID, w.Owner, w.Token, s.now()).Scan(&token) != nil {
			return ErrFence
		}
	}
	return nil
}
func (s *Store) terminal(ctx context.Context, tx *sql.Tx, w Work, state string) error {
	_, e := s.exec(ctx, tx, "UPDATE grab_events_work SET state=?,owner='',lease_until=0,next_at=? WHERE namespace=? AND id=?", state, s.now(), s.Config.Namespace, w.ID)
	if e != nil {
		return ErrStore
	}
	if w.State != "completed" && w.State != "dead" {
		_, e = s.exec(ctx, tx, "UPDATE grab_events_budget SET pending=pending-1,payload_bytes=payload_bytes-? WHERE namespace=?", w.Bytes, s.Config.Namespace)
	}
	if e != nil {
		return ErrStore
	}
	return nil
}
func (s *Store) Complete(ctx context.Context, tx *sql.Tx, w Work) error {
	if e := s.Fence(ctx, tx, w); e != nil {
		return e
	}
	return s.terminal(ctx, tx, w, "completed")
}
func (s *Store) Renew(ctx context.Context, w Work) error {
	tx, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.Fence(ctx, tx, w); e != nil {
		return e
	}
	if _, e = s.exec(ctx, tx, "UPDATE grab_events_work SET lease_until=? WHERE namespace=? AND id=?", s.now()+30000, s.Config.Namespace, w.ID); e != nil {
		return ErrStore
	}
	return commit(tx)
}
func (s *Store) Fail(ctx context.Context, w Work, retry bool, uncertain bool, delay time.Duration) error {
	tx, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.Fence(ctx, tx, w); e != nil {
		return e
	}
	state := "dead"
	if uncertain {
		state = "uncertain"
	} else if retry && w.Attempts < s.Config.MaxAttempts && s.now()+delay.Milliseconds() < w.Deadline {
		state = "retry_wait"
	}
	if state == "dead" {
		e = s.terminal(ctx, tx, w, state)
	} else {
		_, e = s.exec(ctx, tx, "UPDATE grab_events_work SET state=?,next_at=?,owner='',lease_until=0 WHERE namespace=? AND id=?", state, s.now()+delay.Milliseconds(), s.Config.Namespace, w.ID)
	}
	if e != nil {
		return ErrStore
	}
	return commit(tx)
}
func (s *Store) Inspect(ctx context.Context, id string) (Status, error) {
	var v Status
	e := s.row(ctx, s.DB, "SELECT id,state,attempts,next_at FROM grab_events_work WHERE namespace=? AND id=?", s.Config.Namespace, id).Scan(&v.ID, &v.State, &v.Attempts, &v.Next)
	if e != nil {
		return Status{}, ErrStore
	}
	return v, nil
}
func (s *Store) Maintain(ctx context.Context, id, action, outcome, reason string) error {
	if len(reason) < 1 || len(reason) > 1024 || strings.ContainsAny(reason, "\x00\r\n") {
		return ErrStore
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	w, e := scan(s.row(ctx, tx, "SELECT "+columns+" FROM grab_events_work WHERE namespace=? AND id=?", s.Config.Namespace, id))
	if e != nil {
		return ErrStore
	}
	switch action {
	case "retry":
		if w.State != "dead" && !(w.State == "uncertain" && w.Resolved == "not-delivered") {
			return ErrStore
		}
		if w.State == "dead" {
			if e = s.admit(ctx, tx, w.Bytes); e != nil {
				return e
			}
		}
		_, e = s.exec(ctx, tx, "UPDATE grab_events_work SET state='pending',next_at=?,deadline=?,attempts=0,resolved='' WHERE namespace=? AND id=?", s.now(), s.now()+86400000, s.Config.Namespace, id)
	case "resolve":
		if w.State != "uncertain" || (outcome != "delivered" && outcome != "not-delivered") {
			return ErrStore
		}
		if outcome == "delivered" {
			e = s.terminal(ctx, tx, w, "completed")
		} else {
			_, e = s.exec(ctx, tx, "UPDATE grab_events_work SET resolved=? WHERE namespace=? AND id=?", outcome, s.Config.Namespace, id)
		}
	default:
		return ErrStore
	}
	if e != nil {
		return ErrStore
	}
	if _, e = s.exec(ctx, tx, "INSERT INTO grab_events_audit(namespace,id,work_id,action,reason,occurred_at) VALUES(?,?,?,?,?,?)", s.Config.Namespace, ID(), id, action+":"+outcome, []byte(reason), s.now()); e != nil {
		return ErrStore
	}
	return commit(tx)
}

// Purge is explicit, audited, bounded maintenance. No live/uncertain/dead work
// is removed. Completion advances next_at, so tombstones last at least 30 days.
func (s *Store) Purge(ctx context.Context, before int64, reason string) (int, error) {
	if before > s.now()-int64(30*24*time.Hour/time.Millisecond) || len(reason) < 1 || len(reason) > 1024 || strings.ContainsAny(reason, "\x00\r\n") {
		return 0, ErrStore
	}
	tx, e := s.begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, s.q("SELECT id FROM grab_events_work WHERE namespace=? AND state='completed' AND next_at<? ORDER BY next_at,id LIMIT 1000"), s.Config.Namespace, before)
	if e != nil {
		return 0, ErrStore
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return 0, ErrStore
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, ErrStore
	}
	for _, id := range ids {
		if _, e = s.exec(ctx, tx, "DELETE FROM grab_events_work WHERE namespace=? AND id=? AND state='completed'", s.Config.Namespace, id); e != nil {
			return 0, ErrStore
		}
		if _, e = s.exec(ctx, tx, "INSERT INTO grab_events_audit(namespace,id,work_id,action,reason,occurred_at) VALUES(?,?,?,'purge',?,?)", s.Config.Namespace, ID(), id, []byte(reason), s.now()); e != nil {
			return 0, ErrStore
		}
	}
	if e = commit(tx); e != nil {
		return 0, e
	}
	return len(ids), nil
}
func (s *Store) String() string { return fmt.Sprintf("event store (%s)", s.Driver) }
