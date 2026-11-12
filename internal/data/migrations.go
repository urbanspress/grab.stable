package data

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"strings"

	rt "grab/internal/runtime"
)

type MigrationVersion struct {
	Version  int
	Name     string
	Steps    []string
	Checksum string
}

// MigrationManifest validates the complete history before any engine is opened.
// The interpreter additionally requires an AST literal at the language boundary.
func MigrationManifest(v rt.Value, dialect string) ([]MigrationVersion, error) {
	bad := func() ([]MigrationVersion, error) { return nil, fail(Invalid, "invalid literal migration manifest") }
	if v.Kind() != rt.ListKind || v.ListLen() > 1000 {
		return bad()
	}
	out := make([]MigrationVersion, 0, v.ListLen())
	names := map[string]bool{}
	for i, item := range v.ListValues() {
		if item.Kind() != rt.RecordKind || len(item.Entries()) != 3 {
			return bad()
		}
		version, vok := item.LookupEntry("version")
		name, nok := item.LookupEntry("name")
		steps, sok := item.LookupEntry("steps")
		if !vok || !nok || !sok || version.Kind() != rt.IntegerKind || !version.IntegerValue().IsInt64() || version.IntegerValue().Int64() != int64(i+1) || name.Kind() != rt.TextKind || !Identifier(name.TextValue()) || names[name.TextValue()] || steps.Kind() != rt.ListKind || steps.ListLen() == 0 || steps.ListLen() > 100 {
			return bad()
		}
		names[name.TextValue()] = true
		m := MigrationVersion{Version: i + 1, Name: name.TextValue()}
		for _, step := range steps.ListValues() {
			if step.Kind() != rt.TextKind {
				return bad()
			}
			sql := strings.ReplaceAll(step.TextValue(), "\r\n", "\n")
			if strings.ContainsRune(sql, '\r') {
				return bad()
			}
			s, e := ScanSQL(sql, dialect)
			if e != nil || len(s.Parameters) != 0 || !migrationDDL(s) {
				return bad()
			}
			m.Steps = append(m.Steps, sql)
		}
		m.Checksum = migrationChecksum(m)
		out = append(out, m)
	}
	return out, nil
}

// Deliberately accept a transactional schema subset. Views, triggers, virtual
// tables, administrative DDL, CREATE ... AS SELECT and embedded DML are outside
// the initial migration API. The engine policy must still authorize all effects.
func migrationDDL(s SQL) bool {
	for _, name := range s.Identifiers {
		if strings.HasPrefix(strings.ToLower(name), "grab_events_") || strings.EqualFold(name, "grab_schema_migrations") {
			return false
		}
	}
	if len(s.Tokens) < 3 {
		return false
	}
	switch s.Tokens[0] {
	case "CREATE":
		if s.Tokens[1] != "TABLE" && s.Tokens[1] != "INDEX" && !(s.Tokens[1] == "UNIQUE" && s.Tokens[2] == "INDEX") {
			return false
		}
	case "ALTER":
		if s.Tokens[1] != "TABLE" {
			return false
		}
	case "DROP":
		if s.Tokens[1] != "TABLE" && s.Tokens[1] != "INDEX" {
			return false
		}
	default:
		return false
	}
	for i, t := range s.Tokens {
		if t == "SELECT" || t == "ATTACH" || t == "DETACH" || t == "PRAGMA" || t == "GRAB_SCHEMA_MIGRATIONS" || t == "LOAD_EXTENSION" || t == "OUTFILE" || t == "INFILE" {
			return false
		}
		if t == "DEFAULT" && i+1 < len(s.Tokens) && s.Tokens[i+1] == "(" {
			return false
		} // no function defaults
	}
	return true
}

func checksumPart(h hash.Hash, s string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(s)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(s))
}
func migrationChecksum(m MigrationVersion) string {
	h := sha256.New()
	var version [8]byte
	binary.BigEndian.PutUint64(version[:], uint64(m.Version))
	_, _ = h.Write(version[:])
	checksumPart(h, m.Name)
	for _, step := range m.Steps {
		checksumPart(h, step)
	}
	return hex.EncodeToString(h.Sum(nil))
}
