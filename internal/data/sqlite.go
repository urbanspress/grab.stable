package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync/atomic"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// SQLitePolicy is shared only by connections in one host-owned pool. The host
// must hold its exclusive migration lease while changing Maintenance.
type SQLitePolicy struct {
	ReadOnly    bool
	Maintenance atomic.Bool
	Ledger      atomic.Bool
	MaxBytes    int
}

var sqliteSafeFunctions = func() map[string]bool {
	out := map[string]bool{}
	for _, name := range strings.Fields("abs avg changes char coalesce concat concat_ws count date datetime format glob hex if ifnull iif instr julianday last_insert_rowid length like likelihood likely lower ltrim max min nullif octet_length printf quote random randomblob replace round rtrim sign soundex sqlite_version strftime string_agg substr substring sum time timediff total total_changes trim typeof unicode unhex unixepoch unlikely upper zeroblob row_number rank dense_rank percent_rank cume_dist ntile lag lead first_value last_value nth_value") {
		out[name] = true
	}
	return out
}()

func (p *SQLitePolicy) authorize(action sqlite.AuthorizerActionCode, a, b, database, trigger string) sqlite.AuthorizerReturnCode {
	if database != "" && database != "main" && database != "temp" {
		return sqlite.AuthorizerDeny
	}
	maintenance := p.Maintenance.Load() && !p.ReadOnly
	if strings.EqualFold(a, "grab_schema_migrations") && action != sqlite.AuthRead && !p.Ledger.Load() {
		return sqlite.AuthorizerDeny
	}
	switch action {
	case sqlite.AuthSelect, sqlite.AuthRead, sqlite.AuthRecursive, sqlite.AuthTransaction:
		return sqlite.AuthorizerOK
	case sqlite.AuthFunction:
		if sqliteSafeFunctions[strings.ToLower(b)] {
			return sqlite.AuthorizerOK
		}
	case sqlite.AuthInsert, sqlite.AuthUpdate, sqlite.AuthDelete:
		name := strings.ToLower(a)
		if !p.ReadOnly && (!strings.HasPrefix(name, "sqlite_") && name != "grab_schema_migrations" || maintenance) {
			return sqlite.AuthorizerOK
		}
	case sqlite.AuthCreateTable, sqlite.AuthCreateIndex, sqlite.AuthDropTable, sqlite.AuthDropIndex, sqlite.AuthAlterTable, sqlite.AuthReindex:
		if maintenance {
			return sqlite.AuthorizerOK
		}
	}
	// Includes all PRAGMAs, ATTACH/DETACH, savepoints, extensions, virtual
	// tables, temporary DDL, user functions and unsupported future opcodes.
	return sqlite.AuthorizerDeny
}

type sqliteConnector struct {
	driver.Connector
	policy *SQLitePolicy
}

// SQLiteConnector installs engine authorization and safety settings on EVERY
// physical connection, including replacements after pool eviction. The DSN is
// host-constructed configuration, never GRAB source or request input.
func SQLiteConnector(dsn string, policy *SQLitePolicy) (driver.Connector, error) {
	if policy == nil {
		return nil, fail(Invalid, "missing SQLite authorization policy")
	}
	base, err := sqlite.NewConnector(dsn)
	if err != nil {
		return nil, fail(Invalid, "invalid SQLite configuration")
	}
	return &sqliteConnector{base, policy}, nil
}

func (c *sqliteConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, fail(Operation, "database connection unavailable")
	}
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close()
		}
	}()
	exec, supported := conn.(driver.ExecerContext)
	if !supported {
		return nil, fail(Operation, "SQLite driver lacks context execution")
	}
	for _, sql := range []string{"PRAGMA foreign_keys = ON", "PRAGMA temp_store = MEMORY", "PRAGMA trusted_schema = OFF", "PRAGMA busy_timeout = 0"} {
		if _, err = exec.ExecContext(ctx, sql, nil); err != nil {
			return nil, fail(Operation, "database safety configuration failed")
		}
	}
	if c.policy.ReadOnly {
		if _, err = exec.ExecContext(ctx, "PRAGMA query_only = ON", nil); err != nil {
			return nil, fail(Operation, "database read-only configuration failed")
		}
	}
	auth, supported := conn.(sqlite.AuthorizerRegisterer)
	if !supported {
		return nil, fail(Operation, "SQLite driver lacks authorization")
	}
	if err = auth.RegisterAuthorizer(c.policy.authorize); err != nil {
		return nil, fail(Operation, "database authorization configuration failed")
	}
	if err = sqliteConnectionLimits(ctx, conn, c.policy.MaxBytes); err != nil {
		return nil, err
	}
	ok = true
	return conn, nil
}

// The driver's public Limit API accepts *sql.Conn. Borrow a connection solely
// for that call; closing this temporary facade never closes the owned driver
// connection, which is returned to the real host pool only after all limits apply.
type borrowedConnector struct {
	conn   driver.Conn
	driver driver.Driver
}

func (b borrowedConnector) Connect(context.Context) (driver.Conn, error) {
	return borrowedConnection{b.conn}, nil
}
func (b borrowedConnector) Driver() driver.Driver { return b.driver }

type borrowedConnection struct{ driver.Conn }

func (b borrowedConnection) Close() error        { return nil }
func (b borrowedConnection) Unwrap() driver.Conn { return b.Conn }
func sqliteConnectionLimits(ctx context.Context, conn driver.Conn, maxBytes int) error {
	if maxBytes == 0 {
		maxBytes = 10 << 20
	}
	db := sql.OpenDB(borrowedConnector{conn: conn, driver: &sqlite.Driver{}})
	db.SetMaxOpenConns(1)
	defer db.Close()
	c, e := db.Conn(ctx)
	if e != nil {
		return fail(Operation, "database limit configuration failed")
	}
	defer c.Close()
	for _, limit := range []struct{ id, value int }{{sqlite3.SQLITE_LIMIT_LENGTH, maxBytes}, {sqlite3.SQLITE_LIMIT_SQL_LENGTH, 1 << 20}, {sqlite3.SQLITE_LIMIT_VARIABLE_NUMBER, 256}} {
		if _, e = sqlite.Limit(c, limit.id, limit.value); e != nil {
			return fail(Operation, "database limit configuration failed")
		}
	}
	return nil
}

// ValidateStatement is a conservative public-statement classifier. Engine
// authorization remains mandatory; this classifier alone is not a sandbox.
func ValidateStatement(s SQL) (write bool, err error) {
	bad := func() (bool, error) { return false, fail(Invalid, "SQL operation is outside the prepared query API") }
	if len(s.Tokens) == 0 {
		return bad()
	}
	switch s.Tokens[0] {
	case "SELECT":
	case "INSERT", "UPDATE", "DELETE":
		write = true
	case "WITH":
		// WITH has a full nested SQL grammar. Until that grammar is classified,
		// fail closed rather than guessing whether a CTE writes or reads.
		return bad()
	default:
		return bad()
	}
	if write {
		for _, identifier := range append(append([]string(nil), s.Tokens...), s.Identifiers...) {
			if strings.EqualFold(identifier, "grab_schema_migrations") {
				return bad()
			}
		}
	}
	for i, t := range s.Tokens {
		if t == "OUTFILE" || t == "INFILE" || t == "DUMPFILE" || t == "INTO" && s.Tokens[0] != "INSERT" {
			return bad()
		}
		if t == "*" && (i == 1 || i > 0 && (s.Tokens[i-1] == "SELECT" || s.Tokens[i-1] == "." || s.Tokens[i-1] == "," || s.Tokens[i-1] == "DISTINCT" || s.Tokens[i-1] == "ALL")) {
			return bad()
		}
		if t == "RETURNING" { // SQLite RETURNING * also hides schema drift.
			if i+1 < len(s.Tokens) && s.Tokens[i+1] == "*" {
				return bad()
			}
		}
		if t == "(" && i > 0 {
			prev := s.Tokens[i-1]
			if i > 1 && s.Tokens[i-2] == "." {
				return bad()
			}
			if prev == "<quoted>" {
				return bad()
			}
			if Identifier(prev) && !sqliteSafeFunctions[strings.ToLower(prev)] {
				// Table column lists and SQL grouping syntax are not function calls.
				group := prev == "CAST" || prev == "DECIMAL" || prev == "NUMERIC" || prev == "VARCHAR" || prev == "CHAR" || prev == "BINARY" || prev == "IN" || prev == "VALUES" || prev == "EXISTS" || prev == "AS" || prev == "OVER" || prev == "ON" || prev == "WHERE" || prev == "AND" || prev == "OR" || prev == "NOT" || prev == "SELECT" || prev == "FROM" || prev == "USING"
				if !group && !(write && i >= 2 && s.Tokens[i-2] == "INTO") {
					return bad()
				}
			}
		}
	}
	return write, nil
}
