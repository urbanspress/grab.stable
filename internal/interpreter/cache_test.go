package interpreter

import (
	"grab/internal/cache"
	"grab/internal/lexer"
	"testing"
)

func TestCacheUncertainStructuredCause(t *testing.T) {
	e := cacheError(lexer.Span{}, cache.ErrUncertain).(*Error)
	cause, ok := e.ErrorRecord().LookupEntry("cause")
	if !ok || cause.TextValue() != "uncertain" || e.Code() != "GRAB-E314" {
		t.Fatal("uncertain outcome not structured")
	}
}
