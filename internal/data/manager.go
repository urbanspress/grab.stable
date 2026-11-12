package data

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	rt "grab/internal/runtime"
)

// Manager owns one resource generation. Nothing in it is language-mutable.
type Manager struct {
	mu           sync.Mutex
	grants       *Grants
	server       bool
	closed       atomic.Bool
	pools        map[string]*Pool
	transactions map[*sql.Tx]context.CancelFunc
}
type Pool struct {
	owner      *Manager
	db         *sql.DB
	def        definition
	mode       Mode
	policy     *SQLitePolicy
	vfs        interface{ Close() error }
	mu         sync.Mutex
	statements []*Statement
	closed     bool
	active     int
}
type Statement struct {
	hostTransaction *Transaction
	pool            *Pool
	sql             *sql.Stmt
	scanned         SQL
	contract        Contract
	write           bool
}

func (p *Pool) Driver() string { return p.def.Driver }

// Ready checks existing generation pools without creating new connections to
// configured but unused databases or running migrations.
func (m *Manager) Ready(ctx context.Context) bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return false
	}
	for _, p := range m.pools {
		if p.db.PingContext(ctx) != nil {
			return false
		}
	}
	return true
}

type Scope struct {
	hostOwned      bool
	hostStatements []*Statement
	manager        *Manager
	tx             *Transaction
	closed         bool
}
type Transaction struct {
	scope                            *Scope
	pool                             *Pool
	tx                               *sql.Tx
	ctx                              context.Context
	cancel                           context.CancelFunc
	terminal, rollbackOnly, readOnly bool
}

func NewManager(grants *Grants, server bool) *Manager {
	return &Manager{grants: grants, server: server, pools: map[string]*Pool{}, transactions: map[*sql.Tx]context.CancelFunc{}}
}
func (m *Manager) Scope() *Scope { return &Scope{manager: m} }
func (s *Scope) Close() {
	if s == nil || s.closed {
		return
	}
	if s.tx != nil {
		_ = s.Rollback(s.tx)
	}
	for _, st := range s.hostStatements {
		_ = st.sql.Close()
	}
	s.hostStatements = nil
	s.closed = true
}
func (s *Scope) ActiveTransaction() *Transaction { return s.tx }
func (s *Scope) RollbackNew(previous *Transaction) {
	if s.tx != nil && s.tx != previous {
		_ = s.Rollback(s.tx)
	}
}
func (s *Scope) valid(p *Pool) error {
	if s == nil || s.closed || s.manager == nil || s.manager.closed.Load() || p == nil || p.owner != s.manager {
		return fail(Ownership, "database resource has expired or belongs to another execution")
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return fail(Ownership, "database resource is closed")
	}
	return nil
}
func duration(n *int, def int) time.Duration {
	if n != nil {
		def = *n
	}
	return time.Duration(def) * time.Millisecond
}
func bounded(ctx context.Context, p *Pool) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, duration(p.def.Limits.OperationTimeoutMS, 5000))
}
func safeError(ctx context.Context, e error) error {
	if e == nil {
		return nil
	}
	var de *Error
	if errors.As(e, &de) {
		return de
	}
	if ctx.Err() != nil || errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
		return fail(Limit, "database deadline exceeded")
	}
	return driverError(e)
}

func (s *Scope) Open(ctx context.Context, alias string) (*Pool, error) {
	if s == nil || s.closed || s.manager == nil || s.manager.closed.Load() {
		return nil, fail(Ownership, "database execution has expired")
	}
	m := s.manager
	d, mode, e := m.grants.definition(alias)
	if e != nil {
		return nil, e
	} // no I/O before grant
	if m.server && mode == Migrate {
		return nil, fail(Denied, "server executions cannot hold migration grants")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return nil, fail(Ownership, "database execution has expired")
	}
	if p := m.pools[alias]; p != nil {
		if e = s.valid(p); e != nil {
			return nil, e
		}
		return p, nil
	}
	p := &Pool{owner: m, def: d, mode: mode}
	op, cancel := bounded(ctx, p)
	defer cancel()
	if d.Driver == "sqlite" {
		p.policy = &SQLitePolicy{ReadOnly: mode == Read}
		p.policy.MaxBytes = 10 << 20
		if d.Limits.MaxResultBytes != nil {
			p.policy.MaxBytes = *d.Limits.MaxResultBytes
		}
		if d.Limits.MaxValueBytes != nil && *d.Limits.MaxValueBytes > p.policy.MaxBytes {
			p.policy.MaxBytes = *d.Limits.MaxValueBytes
		}
		// SQLite's internal row/schema records also use this engine limit. The
		// public byte budgets below remain exact, including smaller configured caps.
		if p.policy.MaxBytes < 1<<20 {
			p.policy.MaxBytes = 1 << 20
		}
		dsn := ":memory:?_defensive=true&_txlock=immediate"
		if d.Path != ":memory:" {
			canonical, e := canonicalDatabasePath(d.Path)
			if e != nil {
				return nil, e
			}
			v, e := newConfinedVFS(canonical)
			if e != nil {
				return nil, e
			}
			p.vfs = v
			openMode := "rwc"
			if mode == Read {
				openMode = "ro"
			}
			dsn = "file:" + url.PathEscape(filepath.Base(canonical)) + "?vfs=" + v.Name() + "&mode=" + openMode + "&_defensive=true&_txlock=immediate"
		}
		c, e := SQLiteConnector(dsn, p.policy)
		if e != nil {
			if p.vfs != nil {
				_ = p.vfs.Close()
			}
			return nil, e
		}
		p.db = sql.OpenDB(noRetryConnector{c})
	} else {
		password, present := os.LookupEnv(d.PasswordEnv)
		if !present {
			return nil, fail(Denied, "configured database credential is unavailable")
		}
		c, e := remoteConnector(d, mode, password)
		if e != nil {
			return nil, e
		}
		p.db = sql.OpenDB(noRetryConnector{c})
	}
	open, idle, lifetime := 10, 5, 1800000
	if d.Driver == "sqlite" {
		open, idle, lifetime = 1, 1, 0
	}
	if d.Pool.MaxOpen != nil {
		open = *d.Pool.MaxOpen
	}
	if d.Pool.MaxIdle != nil {
		idle = *d.Pool.MaxIdle
	}
	p.db.SetMaxOpenConns(open)
	p.db.SetMaxIdleConns(idle)
	p.db.SetConnMaxLifetime(duration(d.Pool.MaxLifetimeMS, lifetime))
	p.db.SetConnMaxIdleTime(duration(d.Pool.IdleTimeoutMS, 300000))
	if d.Path == ":memory:" {
		p.db.SetConnMaxIdleTime(0)
	}
	if e = p.db.PingContext(op); e != nil {
		_ = p.db.Close()
		if p.vfs != nil {
			_ = p.vfs.Close()
		}
		return nil, safeError(op, e)
	}
	m.pools[alias] = p
	return p, nil
}

func canonicalDatabasePath(path string) (string, error) {
	parent, e := filepath.EvalSymlinks(filepath.Dir(path))
	if e != nil {
		return "", fail(Denied, "database directory is unavailable")
	}
	canonical := filepath.Join(parent, filepath.Base(path))
	for _, part := range strings.FieldsFunc(strings.ToLower(canonical), func(c rune) bool { return c == '/' || c == '\\' }) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, ".env.") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") {
			return "", fail(Denied, "sensitive database path denied")
		}
		switch part {
		case ".ssh", ".aws", ".gnupg", ".codex", ".agents", ".git", ".env", "id_rsa", "id_ed25519":
			return "", fail(Denied, "sensitive database path denied")
		}
	}
	return canonical, nil
}

func (s *Scope) Prepare(ctx context.Context, p *Pool, source string, contract Contract) (*Statement, error) {
	if e := s.valid(p); e != nil {
		return nil, e
	}
	if s.tx != nil && s.tx.pool == p && !s.hostOwned {
		return nil, fail(Ownership, "prepare statements before beginning a transaction")
	}
	sqlText, e := ScanSQL(source, p.def.Driver)
	if e != nil {
		return nil, e
	}
	write, e := ValidateStatement(sqlText)
	if e != nil {
		return nil, e
	}
	if write && p.mode == Read {
		return nil, fail(Denied, "database write capability denied")
	}
	if e = contract.ValidateSQL(sqlText, p.def.Driver); e != nil {
		return nil, e
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	max := 128
	if p.def.Limits.MaxStatements != nil {
		max = *p.def.Limits.MaxStatements
	}
	if len(p.statements)+len(s.hostStatements) >= max {
		return nil, fail(Limit, "prepared statement limit exceeded")
	}
	op, cancel := bounded(ctx, p)
	defer cancel()
	var stmt *sql.Stmt
	if s.hostOwned && s.tx != nil {
		if e = s.transaction(s.tx, p); e != nil {
			return nil, e
		}
		stmt, e = s.tx.tx.PrepareContext(op, sqlText.Text)
	} else {
		stmt, e = p.db.PrepareContext(op, sqlText.Text)
	}
	if e != nil {
		return nil, safeError(op, e)
	}
	st := &Statement{pool: p, sql: stmt, scanned: sqlText, contract: contract, write: write}
	if s.hostOwned && s.tx != nil {
		st.hostTransaction = s.tx
		s.hostStatements = append(s.hostStatements, st)
	} else {
		p.statements = append(p.statements, st)
	}
	return st, nil
}

func (s *Scope) Begin(ctx context.Context, p *Pool, readOnly bool, isolation string) (*Transaction, error) {
	if e := s.valid(p); e != nil {
		return nil, e
	}
	if s.tx != nil {
		return nil, fail(Ownership, "only one transaction may be active per execution")
	}
	if p.mode == Read && !readOnly {
		return nil, fail(Denied, "read grant requires a read-only transaction")
	}
	level := sql.LevelSerializable
	switch isolation {
	case "", "serializable":
	case "read_committed":
		level = sql.LevelReadCommitted
	case "repeatable_read":
		level = sql.LevelRepeatableRead
	default:
		return nil, fail(Ownership, "unsupported transaction isolation")
	}
	if p.def.Driver == "sqlite" && level != sql.LevelSerializable {
		return nil, fail(Ownership, "SQLite supports serializable isolation only")
	}
	parent, cancel := context.WithTimeout(ctx, duration(p.def.Limits.TransactionTimeoutMS, 10000))
	// Bound the initial lease/BEGIN without retaining its shorter operation
	// deadline after success: database/sql retains this context for the tx.
	beginTimer := time.AfterFunc(duration(p.def.Limits.OperationTimeoutMS, 5000), cancel)
	tx, e := p.db.BeginTx(parent, &sql.TxOptions{Isolation: level, ReadOnly: readOnly})
	beginTimer.Stop()
	if e != nil {
		safe := safeError(parent, e)
		cancel()
		return nil, safe
	}
	s.manager.mu.Lock()
	if s.manager.closed.Load() {
		s.manager.mu.Unlock()
		cancel()
		_ = tx.Rollback()
		return nil, fail(Ownership, "database execution has expired")
	}
	s.manager.transactions[tx] = cancel
	s.manager.mu.Unlock()
	t := &Transaction{scope: s, pool: p, tx: tx, ctx: parent, cancel: cancel, readOnly: readOnly}
	s.tx = t
	p.mu.Lock()
	p.active++
	p.mu.Unlock()
	return t, nil
}
func (s *Scope) transaction(t *Transaction, p *Pool) error {
	if t == nil || t.scope != s || t.pool != p || t.terminal || s.tx != t || t.rollbackOnly {
		return fail(Ownership, "invalid, closed or rollback-only transaction")
	}
	if t.ctx.Err() != nil {
		return fail(Limit, "database transaction deadline exceeded")
	}
	return nil
}
func (s *Scope) finish(t *Transaction) {
	s.manager.mu.Lock()
	delete(s.manager.transactions, t.tx)
	s.manager.mu.Unlock()
	t.terminal = true
	t.cancel()
	if s.tx == t {
		s.tx = nil
	}
	t.pool.mu.Lock()
	t.pool.active--
	t.pool.mu.Unlock()
}
func (s *Scope) Commit(t *Transaction) error {
	if t == nil {
		return fail(Ownership, "invalid transaction")
	}
	if e := s.valid(t.pool); e != nil {
		return e
	}
	if e := s.transaction(t, t.pool); e != nil {
		return e
	}
	e := t.tx.Commit()
	s.finish(t)
	if e != nil {
		return fail(UnknownCommit, "commit outcome unknown; reconcile the business operation before retrying")
	}
	return nil
}
func (s *Scope) Rollback(t *Transaction) error {
	if t == nil || t.scope != s {
		return fail(Ownership, "transaction belongs to another execution")
	}
	if t.terminal {
		return nil
	}
	e := t.tx.Rollback()
	s.finish(t)
	if errors.Is(e, sql.ErrTxDone) {
		return nil
	}
	return safeError(context.Background(), e)
}

func (s *Scope) arguments(st *Statement, params rt.Value) ([]any, error) {
	if params.Kind() != rt.RecordKind || len(params.Entries()) != len(st.contract.Params) {
		return nil, fail(Type, "database parameter keys do not match contract")
	}
	bound := map[string]any{}
	for _, f := range st.contract.Params {
		v, ok := params.LookupEntry(f.Name)
		if !ok {
			return nil, fail(Type, "database parameter keys do not match contract")
		}
		raw, e := f.Spec.Bind(v, st.pool.def.Driver)
		if e != nil {
			return nil, e
		}
		bound[f.Name] = raw
	}
	args := make([]any, 0, len(st.scanned.Bindings))
	for _, name := range st.scanned.Bindings {
		args = append(args, bound[name])
	}
	return args, nil
}

func (s *Scope) Query(ctx context.Context, st *Statement, params rt.Value, t *Transaction, one, execute bool) (result rt.Value, err error) {
	if st == nil {
		return result, fail(Ownership, "invalid prepared statement")
	}
	p := st.pool
	if e := s.valid(p); e != nil {
		return result, e
	}
	if t != nil {
		if e := s.transaction(t, p); e != nil {
			return result, e
		}
		defer func() {
			if err != nil {
				t.rollbackOnly = true
			}
		}()
		if st.write && t.readOnly {
			return result, fail(Denied, "read-only transaction cannot write")
		}
	} else if s.tx != nil && s.tx.pool == p {
		return result, fail(Ownership, "active transaction argument is required")
	}
	args, e := s.arguments(st, params)
	if e != nil {
		return result, e
	}
	maxValue := 1 << 20
	if p.def.Limits.MaxValueBytes != nil {
		maxValue = *p.def.Limits.MaxValueBytes
	}
	for _, arg := range args {
		if payloadBytes(arg) > maxValue {
			return result, fail(Limit, "database parameter value limit exceeded")
		}
	}
	op, cancel := bounded(ctx, p)
	defer cancel()
	stmt := st.sql
	if st.hostTransaction != nil && st.hostTransaction != t {
		return result, fail(Ownership, "host statement belongs to another transaction")
	}
	if t != nil && st.hostTransaction == nil {
		stmt = t.tx.StmtContext(op, st.sql)
		defer stmt.Close()
	}
	if execute {
		if len(st.contract.Columns) != 0 {
			return result, fail(Type, "execute requires an empty columns contract")
		}
		// Probe metadata without stepping via a zero-row prepared query is not
		// portable. Static RETURNING detection prevents effects with ignored rows.
		if !st.write || containsToken(st.scanned.Tokens, "RETURNING") {
			return result, fail(Type, "row-returning statement requires query or one")
		}
		r, e := stmt.ExecContext(op, args...)
		if e != nil {
			return result, safeError(op, e)
		}
		n, e := r.RowsAffected()
		if e != nil {
			return result, safeError(op, e)
		}
		return rt.Record([]rt.Entry{{Name: "rows_affected", Value: rt.IntegerFromInt64(n)}}), nil
	}
	if st.write && !containsToken(st.scanned.Tokens, "RETURNING") {
		return result, fail(Type, "non-row-returning write requires execute")
	}
	rows, e := stmt.QueryContext(op, args...)
	if e != nil {
		return result, safeError(op, e)
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		return result, safeError(op, e)
	}
	if len(cols) != len(st.contract.Columns) {
		return result, fail(Type, "result columns do not match contract")
	}
	specs := map[string]TypeSpec{}
	for _, f := range st.contract.Columns {
		specs[f.Name] = f.Spec
	}
	seen := map[string]bool{}
	for _, name := range cols {
		if !Identifier(name) || seen[name] {
			return result, fail(Type, "invalid or duplicate result column")
		}
		seen[name] = true
		if _, ok := specs[name]; !ok {
			return result, fail(Type, "result columns do not match contract")
		}
	}
	metadata, e := rows.ColumnTypes()
	if e != nil {
		return result, safeError(op, e)
	}
	for i, column := range metadata {
		spec := specs[cols[i]]
		if !compatibleColumn(p.def.Driver, column.DatabaseTypeName(), spec) {
			return result, fail(Type, "unsupported database column type or mismatched contract")
		}
		if spec.Type == "decimal" && p.def.Driver != "sqlite" {
			if precision, scale, ok := column.DecimalSize(); ok && (precision != int64(spec.Precision) || scale != int64(spec.Scale)) {
				return result, fail(Type, "database decimal precision or scale does not match contract")
			}
		}
		if spec.Type == "timestamp" && p.def.Driver != "sqlite" {
			if _, fraction, ok := column.DecimalSize(); ok && fraction != int64(spec.FractionDigits) {
				return result, fail(Type, "database timestamp precision does not match contract")
			}
		}
	}
	maxRows, maxBytes := 10000, 10<<20
	if p.def.Limits.MaxRows != nil {
		maxRows = *p.def.Limits.MaxRows
	}
	if p.def.Limits.MaxResultBytes != nil {
		maxBytes = *p.def.Limits.MaxResultBytes
	}
	total := 0
	var list []rt.Value
	for rows.Next() {
		if len(list) >= maxRows || one && len(list) >= 1 {
			return result, fail(Limit, "database row limit exceeded")
		}
		raw := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range raw {
			ptr[i] = &raw[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			return result, safeError(op, e)
		}
		entries := make([]rt.Entry, len(cols))
		for i, name := range cols {
			if p.def.Driver == "postgres" && metadata[i].DatabaseTypeName() == "DATE" && raw[i] != nil {
				date, ok := raw[i].(time.Time)
				if !ok || date.Year() < 1 || date.Year() > 9999 {
					return result, fail(Type, "invalid database date")
				}
				raw[i] = date.Format("2006-01-02")
			}
			size := payloadBytes(raw[i])
			if size > maxValue || size > maxBytes-total {
				return result, fail(Limit, "database result byte limit exceeded")
			}
			value, e := specs[name].Decode(raw[i], p.def.Driver)
			if e != nil {
				return result, e
			}
			// Contracts can pad decimals/timestamps. Account for that materialized
			// payload too, without counting container formatting or quoting.
			size = max(size, valuePayloadBytes(value))
			if size > maxValue || size > maxBytes-total {
				return result, fail(Limit, "database result byte limit exceeded")
			}
			total += size
			entries[i] = rt.Entry{Name: name, Value: value}
		}
		list = append(list, rt.Record(entries))
	}
	if e = rows.Err(); e != nil {
		return result, safeError(op, e)
	}
	if one {
		if len(list) == 0 {
			return rt.Nothing(), nil
		}
		return list[0], nil
	}
	return rt.List(list), nil
}
func payloadBytes(v any) int {
	switch x := v.(type) {
	case nil:
		return 0
	case string:
		return len(x)
	case []byte:
		return len(x)
	case bool:
		return 1
	case int64:
		return len(rt.IntegerFromInt64(x).Display())
	case uint64:
		return len(new(big.Int).SetUint64(x).String())
	case float64:
		v, e := rt.FloatFromFloat64(x)
		if e != nil {
			return 32
		}
		return len(v.Display())
	case time.Time:
		return len(x.Format(time.RFC3339Nano))
	default:
		return 32
	}
}

func valuePayloadBytes(v rt.Value) int {
	switch v.Kind() {
	case rt.NothingKind:
		return 0
	case rt.TextKind:
		return len(v.TextValue())
	case rt.BytesKind:
		return len(v.BytesValue())
	case rt.BooleanKind:
		return 1
	default:
		return len(v.Display())
	}
}
func containsToken(tokens []string, want string) bool {
	for _, t := range tokens {
		if t == want {
			return true
		}
	}
	return false
}

func (s *Scope) ClosePool(p *Pool) error {
	if s != nil && !s.closed && s.manager != nil && !s.manager.closed.Load() && p != nil && p.owner == s.manager && !s.manager.server {
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if closed {
			return nil
		}
	}
	if e := s.valid(p); e != nil {
		return e
	}
	if s.manager.server {
		return fail(Ownership, "server pools are host-owned")
	}
	return p.close(false)
}
func (p *Pool) close(force bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	if p.active > 0 && !force {
		return fail(Ownership, "database has active transactions")
	}
	p.closed = true
	for _, st := range p.statements {
		_ = st.sql.Close()
	}
	e := p.db.Close()
	if p.vfs != nil {
		if ve := p.vfs.Close(); e == nil {
			e = ve
		}
	}
	return safeError(context.Background(), e)
}
func (m *Manager) Close() error {
	if m == nil || m.closed.Swap(true) {
		return nil
	}
	m.mu.Lock()
	for tx, cancel := range m.transactions {
		cancel()
		_ = tx.Rollback()
	}
	pools := make([]*Pool, 0, len(m.pools))
	for _, p := range m.pools {
		pools = append(pools, p)
	}
	m.mu.Unlock()
	var first error
	for _, p := range pools {
		if e := p.close(true); first == nil {
			first = e
		}
	}
	return first
}

func (m *Manager) PoolUse() int64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, p := range m.pools {
		n += int64(p.db.Stats().InUse)
	}
	return n
}
