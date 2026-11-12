package data

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// SQL is a scanned single statement. ScanSQL handles lexical separation and
// placeholder rewriting; it is deliberately NOT a database authorization layer.
// An adapter must also enforce engine authorization and read-only connections.
type SQL struct {
	Text        string
	Parameters  []string // distinct names in first appearance order
	Bindings    []string // driver argument order (MariaDB repeats values)
	Tokens      []string // unquoted words and punctuation, for static validation
	Identifiers []string // decoded quoted identifiers, separate from string data
}

func identifierStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func identifierPart(c byte) bool  { return identifierStart(c) || c >= '0' && c <= '9' }

func Identifier(s string) bool {
	if len(s) == 0 || !identifierStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !identifierPart(s[i]) {
			return false
		}
	}
	return true
}

// ScanSQL rewrites only real :name parameters. Values never enter this function.
func ScanSQL(source, dialect string) (SQL, error) {
	var result SQL
	bad := func() (SQL, error) { return SQL{}, fail(Invalid, "invalid single SQL statement") }
	if dialect != "sqlite" && dialect != "mariadb" && dialect != "postgres" {
		return bad()
	}
	if len(source) == 0 || len(source) > 1<<20 || !utf8.ValidString(source) || strings.IndexByte(source, 0) >= 0 {
		return bad()
	}
	var out strings.Builder
	indexes := map[string]int{}
	terminated := false
	for i := 0; i < len(source); {
		start := i
		c := source[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			out.WriteByte(c)
			i++
			continue
		}
		if strings.HasPrefix(source[i:], "--") || dialect == "mariadb" && c == '#' {
			if dialect == "mariadb" && c == '-' && i+2 < len(source) && source[i+2] > ' ' {
				return bad()
			}
			for i < len(source) && source[i] != '\n' {
				i++
			}
			out.WriteString(source[start:i])
			continue
		}
		if strings.HasPrefix(source[i:], "/*") {
			// MySQL executable comments alter SQL semantics and are never comments
			// for our purposes. Optimizer hints are also outside the initial API.
			if strings.HasPrefix(source[i:], "/*!") || strings.HasPrefix(source[i:], "/*+") || strings.HasPrefix(source[i:], "/*M!") {
				return bad()
			}
			i += 2
			depth := 1
			for i < len(source) && depth > 0 {
				if strings.HasPrefix(source[i:], "/*") {
					if dialect != "postgres" {
						return bad()
					}
					depth++
					i += 2
				} else if strings.HasPrefix(source[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return bad()
			}
			out.WriteString(source[start:i])
			continue
		}
		if terminated {
			return bad()
		}
		if c == ';' {
			terminated = true
			out.WriteByte(c)
			i++
			continue
		}
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			if c == '`' && dialect == "postgres" || c == '[' && dialect != "sqlite" {
				return bad()
			}
			end := c
			if c == '[' {
				end = ']'
			}
			i++
			closed := false
			for i < len(source) {
				// Backslash escape semantics vary by session/dialect; reject them
				// rather than allowing scanner/server disagreement.
				if source[i] == '\\' {
					return bad()
				}
				if source[i] == end {
					i++
					if i < len(source) && source[i] == end && end != ']' {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return bad()
			}
			out.WriteString(source[start:i])
			if c != '\'' {
				raw := source[start+1 : i-1]
				if c != '[' {
					raw = strings.ReplaceAll(raw, string([]byte{end, end}), string(end))
				}
				result.Identifiers = append(result.Identifiers, raw)
			}
			result.Tokens = append(result.Tokens, "<quoted>")
			continue
		}
		if c == '$' && dialect == "postgres" {
			j := i + 1
			if j < len(source) && identifierStart(source[j]) {
				for j < len(source) && identifierPart(source[j]) {
					j++
				}
			}
			if j < len(source) && source[j] == '$' {
				delim := source[i : j+1]
				k := strings.Index(source[j+1:], delim)
				if k < 0 {
					return bad()
				}
				i = j + 1 + k + len(delim)
				out.WriteString(source[start:i])
				result.Tokens = append(result.Tokens, "<quoted>")
				continue
			}
			return bad() // raw driver placeholders are not part of the portable API
		}
		if c == ':' && strings.HasPrefix(source[i:], "::") && dialect == "postgres" {
			out.WriteString("::")
			result.Tokens = append(result.Tokens, "::")
			i += 2
			continue
		}
		if c == ':' {
			i++
			if i == len(source) || !identifierStart(source[i]) {
				return bad()
			}
			j := i
			for i < len(source) && identifierPart(source[i]) {
				i++
			}
			name := source[j:i]
			index, exists := indexes[name]
			if !exists {
				index = len(indexes) + 1
				indexes[name] = index
				result.Parameters = append(result.Parameters, name)
				if index > 256 {
					return bad()
				}
			}
			if dialect == "mariadb" {
				out.WriteByte('?')
				result.Bindings = append(result.Bindings, name)
			} else {
				prefix := "?"
				if dialect == "postgres" {
					prefix = "$"
				}
				out.WriteString(prefix + strconv.Itoa(index))
			}
			result.Tokens = append(result.Tokens, "<param>")
			continue
		}
		if c == '?' || c == '@' || c == '$' || c == '\\' || c >= 128 {
			return bad()
		}
		if identifierStart(c) {
			i++
			for i < len(source) && identifierPart(source[i]) {
				i++
			}
			token := strings.ToUpper(source[start:i])
			result.Tokens = append(result.Tokens, token)
			out.WriteString(source[start:i])
			continue
		}
		result.Tokens = append(result.Tokens, string(c))
		out.WriteByte(c)
		i++
	}
	if len(result.Tokens) == 0 {
		return bad()
	}
	if dialect != "mariadb" {
		result.Bindings = append([]string(nil), result.Parameters...)
	}
	result.Text = out.String()
	return result, nil
}
