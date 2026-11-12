package interpreter

import (
	"errors"
	"reflect"
	"strconv"

	"grab/internal/ast"
	"grab/internal/cache"
	"grab/internal/lexer"
	"grab/internal/platform"
	rt "grab/internal/runtime"
)

var cacheFunctions = map[string]stdlibFunc{
	"open":   {arity: 1, fn: stdCacheOpen},
	"get":    {arity: 2, fn: stdCacheGet},
	"set":    {arity: 4, fn: stdCacheSet},
	"delete": {arity: 2, fn: stdCacheDelete},
	"limit":  {arity: 4, fn: stdCacheLimit},
}

func cacheError(span lexer.Span, e error) error {
	if e == nil {
		return nil
	}
	code := "GRAB-E314"
	kind := StandardLibraryError
	if errors.Is(e, platform.ErrDenied) {
		code = "GRAB-E403"
		kind = PermissionDenied
	}
	return &Error{Kind: kind, CustomCode: code, Message: e.Error(), Span: span, Err: e}
}
func validateCacheCall(name string, args []ast.Expression, span lexer.Span) error {
	if name != "open" {
		return nil
	}
	if len(args) == 1 {
		if lit, ok := args[0].(*ast.LiteralExpr); ok && lit.Kind == ast.TextLiteral {
			if s, e := strconv.Unquote(lit.Lexeme); e == nil && platform.Identifier(s) {
				return nil
			}
		}
	}
	return &Error{Kind: StandardLibraryError, CustomCode: "GRAB-E210", Message: "cache alias must be a valid direct text literal", Span: span}
}

func containsCacheHandle(v rt.Value) bool {
	if _, ok := v.ResourceValue().(*cache.Handle); ok {
		return true
	}
	for _, x := range v.ListValues() {
		if containsCacheHandle(x) {
			return true
		}
	}
	for _, x := range v.Entries() {
		if containsCacheHandle(x.Value) {
			return true
		}
	}
	return false
}

// ValidatePlatform validates references even in unexecuted branches/functions.
// It performs no adapter I/O, secret reads, or grant escalation.
func ValidatePlatform(program *ast.Program, cfg *platform.Config) error {
	return validatePlatform(program, cfg, nil)
}

func validatePlatform(program *ast.Program, cfg *platform.Config, visit func(string) error) error {
	seen := map[string]bool{}
	var walk func(reflect.Value) error
	walk = func(v reflect.Value) error {
		if !v.IsValid() {
			return nil
		}
		if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if !v.IsNil() {
				return walk(v.Elem())
			}
			return nil
		}
		if v.CanInterface() {
			var module, name string
			var args []ast.Expression
			var span lexer.Span
			switch n := v.Interface().(type) {
			case ast.CallExpr:
				module, name, args, span = n.Module, n.Name, n.Arguments, n.Span
			case ast.CallStatement:
				module, name, args, span = n.Module, n.Name, n.Arguments, n.Span
			}
			if module == "cache" && name == "open" {
				if e := validateCacheCall(name, args, span); e != nil {
					return e
				}
				lit := args[0].(*ast.LiteralExpr)
				alias, _ := strconv.Unquote(lit.Lexeme)
				if !cfg.HasCache(alias) {
					return &Error{Kind: StandardLibraryError, CustomCode: "GRAB-E210", Message: "cache alias is not configured", Span: span}
				}
				if visit != nil && !seen[alias] {
					seen[alias] = true
					if e := visit(alias); e != nil {
						return cacheError(span, e)
					}
				}
			}
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if e := walk(v.Field(i)); e != nil {
					return e
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				if e := walk(v.Index(i)); e != nil {
					return e
				}
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(program))
}
func stdCacheOpen(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	h, e := c.cacheManager.Open(c.context(), a[0].TextValue())
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	return rt.Resource(h), nil
}
func cacheArgs(a []rt.Value) (*cache.Handle, string, error) {
	h, ok := a[0].ResourceValue().(*cache.Handle)
	if !ok || h == nil || a[1].Kind() != rt.TextKind {
		return nil, "", cache.ErrValue
	}
	return h, a[1].TextValue(), nil
}
func cacheInteger(v rt.Value) (int64, error) {
	if v.Kind() != rt.IntegerKind || !v.IntegerValue().IsInt64() {
		return 0, cache.ErrValue
	}
	return v.IntegerValue().Int64(), nil
}
func stdCacheGet(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	h, k, e := cacheArgs(a)
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	r, e := c.cacheManager.Get(c.context(), h, k)
	if e != nil {
		c.metrics.Add("cache_errors", 1)
	} else if r.Found {
		c.metrics.Add("cache_hits", 1)
	}
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	return rt.Record([]rt.Entry{{Name: "found", Value: rt.Boolean(r.Found)}, {Name: "value", Value: r.Value}, {Name: "ttl_ms", Value: rt.IntegerFromInt64(r.TTLMS)}}), nil
}
func stdCacheSet(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	h, k, e := cacheArgs(a)
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	ttl, e := cacheInteger(a[3])
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	return rt.Nothing(), cacheError(s, c.cacheManager.Set(c.context(), h, k, a[2], ttl))
}
func stdCacheDelete(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	h, k, e := cacheArgs(a)
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	removed, e := c.cacheManager.Delete(c.context(), h, k)
	return rt.Boolean(removed), cacheError(s, e)
}
func stdCacheLimit(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	h, k, e := cacheArgs(a)
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	maximum, e := cacheInteger(a[2])
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	window, e := cacheInteger(a[3])
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	r, e := c.cacheManager.Limit(c.context(), h, k, maximum, window)
	if e != nil {
		c.metrics.Add("cache_errors", 1)
	} else if !r.Allowed {
		c.metrics.Add("rate_denials", 1)
	}
	if e != nil {
		return rt.Value{}, cacheError(s, e)
	}
	return rt.Record([]rt.Entry{{Name: "allowed", Value: rt.Boolean(r.Allowed)}, {Name: "count", Value: rt.IntegerFromInt64(r.Count)}, {Name: "remaining", Value: rt.IntegerFromInt64(r.Remaining)}, {Name: "retry_after_ms", Value: rt.IntegerFromInt64(r.RetryAfterMS)}}), nil
}

// walkCalls visits static references, including unreachable code.
func walkCalls(p *ast.Program, visit func(string, string, []ast.Expression, lexer.Span) error) error {
	var walk func(reflect.Value) error
	walk = func(v reflect.Value) error {
		if !v.IsValid() {
			return nil
		}
		if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if !v.IsNil() {
				return walk(v.Elem())
			}
			return nil
		}
		if v.CanInterface() {
			switch n := v.Interface().(type) {
			case ast.CallExpr:
				if e := visit(n.Module, n.Name, n.Arguments, n.Span); e != nil {
					return e
				}
			case ast.CallStatement:
				if e := visit(n.Module, n.Name, n.Arguments, n.Span); e != nil {
					return e
				}
			}
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if e := walk(v.Field(i)); e != nil {
					return e
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				if e := walk(v.Index(i)); e != nil {
					return e
				}
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(p))
}
