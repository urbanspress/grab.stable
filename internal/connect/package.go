// Package connect implements vendored data-only connector packages. Package
// integrity and grants are checked before credentials or transport are used.
package connect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"grab/internal/contracts"
	"grab/internal/platform"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var ErrInvalid = errors.New("GRAB-E210: invalid connector package or integrity lock")
var ErrOperation = errors.New("GRAB-E316: connector operation failed")
var ErrUncertain = errors.New("GRAB-E316: connector outcome uncertain")

type Auth struct {
	Type     string `json:"type"`
	Secret   string `json:"secret"`
	Header   string `json:"header,omitempty"`
	Username string `json:"username,omitempty"`
}
type Mapping struct {
	From    string          `json:"from,omitempty"`
	Literal json.RawMessage `json:"literal,omitempty"`
}
type Operation struct {
	Method            string             `json:"method"`
	Path              string             `json:"path"`
	Input             contracts.Schema   `json:"input"`
	Output            contracts.Schema   `json:"output"`
	PathFields        map[string]string  `json:"path_fields,omitempty"`
	QueryFields       map[string]string  `json:"query_fields,omitempty"`
	BodyFields        map[string]Mapping `json:"body_fields,omitempty"`
	ResponseFields    map[string]string  `json:"response_fields,omitempty"`
	Headers           map[string]string  `json:"headers,omitempty"`
	Auth              *Auth              `json:"auth,omitempty"`
	Classification    string             `json:"classification"`
	IdempotencyHeader string             `json:"idempotency_header,omitempty"`
	IdempotencySource string             `json:"idempotency_source,omitempty"`
	TransientStatuses []int              `json:"transient_statuses,omitempty"`
	TimeoutMS         int                `json:"timeout_ms"`
	MaxBody           int                `json:"max_body_bytes"`
}
type Manifest struct {
	Version         int                         `json:"version"`
	ID              string                      `json:"id"`
	PackageVersion  string                      `json:"package_version"`
	ProviderVersion string                      `json:"provider_version"`
	Sources         []string                    `json:"sources"`
	Origin          string                      `json:"origin"`
	Types           map[string]contracts.Schema `json:"types"`
	Operations      map[string]Operation        `json:"operations"`
	Verifier        string                      `json:"verifier,omitempty"`
}
type LockedPackage struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}
type Lock struct {
	Version  int                      `json:"version"`
	Packages map[string]LockedPackage `json:"packages"`
}
type Package struct {
	Manifest  Manifest
	Contracts *contracts.Document
	Binding   platform.Connector
}

var semver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func files(root, dir string) (map[string]string, error) {
	out := map[string]string{}
	e := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return ErrInvalid
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrInvalid
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(filepath.Join(root, dir), path)
		if e != nil {
			return ErrInvalid
		}
		rel = filepath.ToSlash(rel)
		if rel != "README" && rel != "README.md" && !strings.HasSuffix(rel, ".json") {
			return ErrInvalid
		}
		b, e := platform.ConfinedFile(root, filepath.ToSlash(filepath.Join(dir, rel)))
		if e != nil {
			return ErrInvalid
		}
		out[rel] = hash(b)
		if len(out) > 1000 {
			return ErrInvalid
		}
		return nil
	})
	return out, e
}

// BuildLock is an explicit offline maintenance action; runtime never updates it.
func BuildLock(c *platform.Config) ([]byte, error) {
	l := Lock{Version: 1, Packages: map[string]LockedPackage{}}
	for _, b := range c.Connectors() {
		raw, e := platform.ConfinedFile(c.Root(), filepath.ToSlash(filepath.Join(b.Package, "connector.json")))
		if e != nil {
			return nil, e
		}
		if _, e = Parse(raw, b); e != nil {
			return nil, e
		}
		f, e := files(c.Root(), b.Package)
		if e != nil {
			return nil, e
		}
		l.Packages[b.Package] = LockedPackage{Version: b.Version, Files: f}
	}
	return json.MarshalIndent(l, "", "  ")
}
func Load(c *platform.Config) (map[string]*Package, error) {
	out := map[string]*Package{}
	if len(c.Connectors()) == 0 {
		return out, nil
	}
	b, e := platform.ConfinedFile(c.Root(), "grab.connect.lock")
	if e != nil {
		return nil, ErrInvalid
	}
	var l Lock
	if platform.StrictJSON(b, &l) != nil || l.Version != 1 {
		return nil, ErrInvalid
	}
	for alias, binding := range c.Connectors() {
		locked, ok := l.Packages[binding.Package]
		if !ok || locked.Version != binding.Version {
			return nil, ErrInvalid
		}
		actual, e := files(c.Root(), binding.Package)
		if e != nil || len(actual) != len(locked.Files) {
			return nil, ErrInvalid
		}
		for n, h := range actual {
			if locked.Files[n] != h {
				return nil, ErrInvalid
			}
		}
		raw, e := platform.ConfinedFile(c.Root(), filepath.ToSlash(filepath.Join(binding.Package, "connector.json")))
		if e != nil {
			return nil, ErrInvalid
		}
		p, e := Parse(raw, binding)
		if e != nil {
			return nil, e
		}
		out[alias] = p
	}
	return out, nil
}
func Parse(raw []byte, b platform.Connector) (*Package, error) {
	var m Manifest
	if platform.StrictJSON(raw, &m) != nil || m.Version != 1 || !platform.Identifier(m.ID) || !semver.MatchString(m.PackageVersion) || m.PackageVersion != b.Version || m.ProviderVersion == "" || len(m.Sources) == 0 || len(m.Operations) == 0 || len(m.Operations) > 100 || m.Verifier != "" && m.Verifier != "grab-hmac-sha256-v1" {
		return nil, ErrInvalid
	}
	u, e := url.Parse(m.Origin)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Scheme != "https" && !(b.Development && u.Scheme == "http" && literalLoopback(u.Hostname())) {
		return nil, ErrInvalid
	}
	for _, s := range m.Sources {
		u, e := url.Parse(s)
		if e != nil || u.Scheme != "https" || u.Host == "" {
			return nil, ErrInvalid
		}
	}
	if m.Types == nil {
		m.Types = map[string]contracts.Schema{}
	}
	d := contracts.Document{Version: 1, Operations: []contracts.Operation{}, Types: map[string]contracts.Schema{}}
	for k, v := range m.Types {
		d.Types[k] = v
	}
	for id, o := range m.Operations {
		if !platform.Identifier(id) || !strings.HasPrefix(o.Path, "/") || strings.ContainsAny(o.Path, "?#\\\x00\r\n ") {
			return nil, ErrInvalid
		}
		switch o.Method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
		default:
			return nil, ErrInvalid
		}
		if o.TimeoutMS < 1 || o.TimeoutMS > 60000 || o.MaxBody < 1 || o.MaxBody > 8<<20 {
			return nil, ErrInvalid
		}
		switch o.Classification {
		case "read-only":
			if o.Method != "GET" && o.Method != "HEAD" {
				return nil, ErrInvalid
			}
		case "idempotent":
			if !safeHeader(o.IdempotencyHeader) || o.IdempotencySource == "" {
				return nil, ErrInvalid
			}
		case "unsafe":
		default:
			return nil, ErrInvalid
		}
		seen := map[string]bool{}
		for _, s := range strings.Split(o.Path, "/") {
			if s == "." || s == ".." {
				return nil, ErrInvalid
			}
			if strings.ContainsAny(s, "{}") {
				if len(s) < 3 || s[0] != '{' || s[len(s)-1] != '}' {
					return nil, ErrInvalid
				}
				n := s[1 : len(s)-1]
				if o.PathFields[n] == "" || seen[n] {
					return nil, ErrInvalid
				}
				seen[n] = true
			}
		}
		if len(seen) != len(o.PathFields) {
			return nil, ErrInvalid
		}
		for k, v := range o.Headers {
			if !safeHeader(k) || strings.ContainsAny(v, "\r\n\x00") {
				return nil, ErrInvalid
			}
		}
		if o.Auth != nil {
			a := o.Auth
			if b.Secrets[a.Secret] == "" {
				return nil, ErrInvalid
			}
			switch a.Type {
			case "api-key":
				if !safeHeader(a.Header) {
					return nil, ErrInvalid
				}
			case "bearer", "basic":
			default:
				return nil, ErrInvalid
			}
		}
		for _, status := range o.TransientStatuses {
			if status < 500 || status > 599 {
				return nil, ErrInvalid
			}
		}
		for _, v := range o.BodyFields {
			if (v.From == "") == (len(v.Literal) == 0) {
				return nil, ErrInvalid
			}
		}
		// Reuse the same schema validator without inventing a second type system.
		d.Types["_input_"+strings.ReplaceAll(id, "-", "_")] = o.Input
		d.Types["_output_"+strings.ReplaceAll(id, "-", "_")] = o.Output
	}
	rawD, _ := json.Marshal(d)
	checked, e := contracts.Parse(rawD)
	if e != nil {
		return nil, ErrInvalid
	}
	return &Package{Manifest: m, Contracts: checked, Binding: b}, nil
}
func safeHeader(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	switch http.CanonicalHeaderKey(s) {
	case "Host", "Connection", "Content-Length", "Transfer-Encoding", "Upgrade", "Proxy-Authorization", "Proxy-Authenticate", "Te", "Trailer", "Keep-Alive", "Authorization", "Cookie", "Set-Cookie":
		return false
	}
	return true
}
func IDs(packages map[string]*Package) []string {
	var out []string
	for a, p := range packages {
		for o := range p.Manifest.Operations {
			out = append(out, a+":"+o)
		}
	}
	sort.Strings(out)
	return out
}
