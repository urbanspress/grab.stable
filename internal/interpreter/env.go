package interpreter

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

const maxEnvVarNameLen = 128

var envFunctions = map[string]stdlibFunc{
	"get": {arity: 1, fn: stdEnvGet},
	"has": {arity: 1, fn: stdEnvHas},
}

// ParseAllowEnvList parses and validates a comma-separated --allow-env flag
// value, returning valid POSIX/Windows identifier variable names and whether
// the value was syntactically valid.
func ParseAllowEnvList(raw string) ([]string, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if !isValidEnvVarName(name) {
			return nil, false
		}
		if _, exists := seen[name]; !exists {
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func isValidEnvVarName(name string) bool {
	if len(name) == 0 || len(name) > maxEnvVarNameLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_') {
				return false
			}
			continue
		}
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func (p Permissions) isEnvAllowed(name string) bool {
	if !isValidEnvVarName(name) || len(p.AllowEnv) == 0 {
		return false
	}
	return slices.Contains(p.AllowEnv, name)
}

func checkEnvCapability(ctx *execContext, span lexer.Span, arg grabrt.Value) (string, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return "", err
	}
	if arg.Kind() != grabrt.TextKind {
		return "", &Error{Kind: TypeMismatch, Span: span}
	}
	name := arg.TextValue()
	if ctx == nil || !ctx.permissions.isEnvAllowed(name) {
		return "", &Error{
			Kind: PermissionDenied,
			Span: span,
			Err:  fmt.Errorf("environment variable %q is not allowed (grant with --allow-env %s)", name, name),
		}
	}
	return name, nil
}

func stdEnvGet(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	name, err := checkEnvCapability(ctx, span, args[0])
	if err != nil {
		return grabrt.Value{}, err
	}
	val, ok := os.LookupEnv(name)
	if !ok {
		return grabrt.Nothing(), nil
	}
	if !utf8.ValidString(val) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("environment variable %q is not valid UTF-8", name)}
	}
	return grabrt.Text(val), nil
}

func stdEnvHas(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	name, err := checkEnvCapability(ctx, span, args[0])
	if err != nil {
		return grabrt.Value{}, err
	}
	_, ok := os.LookupEnv(name)
	return grabrt.Boolean(ok), nil
}
