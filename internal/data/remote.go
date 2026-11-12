package data

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql/driver"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// pinnedDial re-resolves and checks every reconnect, then dials a checked literal
// IP. A configured literal may be private/loopback; DNS names may not resolve
// to private addresses. Metadata/link-local endpoints are always denied.
func pinnedDial(d definition) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != d.endpoint() {
			return nil, fail(Denied, "database endpoint denied")
		}
		literal := net.ParseIP(d.Host)
		ips := []net.IP{literal}
		if literal == nil {
			resolved, e := net.DefaultResolver.LookupIPAddr(ctx, d.Host)
			if e != nil {
				return nil, fail(Operation, "database endpoint unavailable")
			}
			ips = nil
			for _, ip := range resolved {
				ips = append(ips, ip.IP)
			}
		}
		if len(ips) == 0 {
			return nil, fail(Operation, "database endpoint unavailable")
		}
		for _, ip := range ips {
			if blockedDatabaseIP(ip) || literal == nil && (ip.IsPrivate() || ip.IsLoopback()) {
				return nil, fail(Denied, "database endpoint denied")
			}
		}
		// No automatic alternate-host retries: one checked destination per dial.
		return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ips[0].String(), strconv.Itoa(d.Port)))
	}
}

func blockedDatabaseIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if v := ip.To4(); v != nil {
		return v[0] == 0 || v[0] == 255 && v[1] == 255 && v[2] == 255 && v[3] == 255
	}
	return false
}

func remoteTLS(d definition) (*tls.Config, error) {
	c := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: d.Host}
	if d.TLSCA == "" {
		return c, nil
	}
	// Only called after the alias grant is checked. Keep a rooted, bounded read
	// of the one operator-selected certificate file; never use GRAB file grants.
	root, e := os.OpenRoot(filepath.Dir(d.TLSCA))
	if e != nil {
		return nil, fail(Denied, "configured database CA is unavailable")
	}
	defer root.Close()
	name := filepath.Base(d.TLSCA)
	before, e := root.Lstat(name)
	if e != nil || !before.Mode().IsRegular() {
		return nil, fail(Denied, "configured database CA is unavailable")
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, fail(Denied, "configured database CA is unavailable")
	}
	defer f.Close()
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		return nil, fail(Denied, "configured database CA is unavailable")
	}
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return nil, fail(Invalid, "invalid database CA")
	}
	c.RootCAs = x509.NewCertPool()
	if !c.RootCAs.AppendCertsFromPEM(raw) {
		return nil, fail(Invalid, "invalid database CA")
	}
	return c, nil
}

func remoteConnector(d definition, mode Mode, password string) (driver.Connector, error) {
	tlsConfig, e := remoteTLS(d)
	if e != nil {
		return nil, e
	}
	if d.Driver == "mariadb" {
		c := mysql.NewConfig()
		c.User = d.User
		c.Passwd = password
		c.DBName = d.Database
		c.Net = "tcp"
		c.Addr = d.endpoint()
		c.DialFunc = pinnedDial(d)
		c.Timeout = 5 * time.Second
		c.MultiStatements = false
		c.InterpolateParams = false
		c.AllowAllFiles = false
		c.AllowCleartextPasswords = false
		c.AllowFallbackToPlaintext = false
		c.ParseTime = false
		c.Logger = silentDriverLogger{}
		c.Params = map[string]string{"time_zone": "'+00:00'", "sql_mode": "'STRICT_ALL_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO'"}
		timeout := 5000
		if d.Limits.OperationTimeoutMS != nil {
			timeout = *d.Limits.OperationTimeoutMS
		}
		c.Params["max_statement_time"] = strconv.FormatFloat(float64(timeout)/1000, 'f', 3, 64)
		if mode == Read {
			c.Params["tx_read_only"] = "1"
		}
		if d.TLS == "verify" {
			c.TLS = tlsConfig
		} else {
			c.TLSConfig = "false"
		}
		connector, e := mysql.NewConnector(c)
		if e != nil {
			return nil, fail(Invalid, "invalid database adapter configuration")
		}
		return connector, nil
	}
	// ParseConfig establishes pgx's required internal invariants. Every endpoint,
	// credential, TLS fallback and session setting is then explicitly replaced;
	// no environment DSN, service file or plaintext fallback can select a target.
	// pgx's parser consults PGSERVICE before returning a config, so overriding
	// the returned endpoint alone would still allow an implicit service-file
	// read. Refuse inherited file selectors; a fixed nonempty bootstrap password
	// prevents .pgpass discovery, and disabled bootstrap TLS avoids client files.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "PGSERVICE", "PGSERVICEFILE", "PGPASSFILE", "PGSSLCERT", "PGSSLKEY", "PGSSLROOTCERT", "PGSSLCRL", "PGSSLCRLDIR":
			return nil, fail(Denied, "inherited PostgreSQL file configuration is not permitted")
		}
	}
	c, e := pgx.ParseConfig("postgres://grab_bootstrap:unused@localhost:5432/postgres?sslmode=disable&target_session_attrs=any")
	if e != nil {
		return nil, fail(Invalid, "invalid database adapter configuration")
	}
	c.Host = d.Host
	c.Port = uint16(d.Port)
	c.Database = d.Database
	c.User = d.User
	c.Password = password
	c.Fallbacks = nil
	c.ValidateConnect = nil
	c.DialFunc = pinnedDial(d)
	c.ConnectTimeout = 5 * time.Second
	c.TLSConfig = nil
	if d.TLS == "verify" {
		c.TLSConfig = tlsConfig
	}
	c.LookupFunc = func(_ context.Context, host string) ([]string, error) { return []string{host}, nil }
	// Omitting pg_catalog from the explicit list keeps its implicit lookup
	// priority while making public the target for ordinary table creation.
	c.RuntimeParams = map[string]string{"timezone": "UTC", "statement_timeout": "5000", "lock_timeout": "5000", "search_path": "public"}
	if d.Limits.OperationTimeoutMS != nil {
		timeout := strconv.Itoa(*d.Limits.OperationTimeoutMS)
		c.RuntimeParams["statement_timeout"] = timeout
		c.RuntimeParams["lock_timeout"] = timeout
	}
	if mode == Read {
		c.RuntimeParams["default_transaction_read_only"] = "on"
	}
	c.StatementCacheCapacity = 0
	c.DescriptionCacheCapacity = 0
	c.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec
	return stdlib.GetConnector(*c), nil
}

// Drivers must not print raw server errors or connection details to stderr.
type silentDriverLogger struct{}

func (silentDriverLogger) Print(...any) {}
