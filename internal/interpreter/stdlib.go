package interpreter

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

type stdlibFunc struct {
	arity    int
	minArity int
	maxArity int
	fn       func(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error)
}

func (f stdlibFunc) acceptsArity(n int) bool {
	if f.maxArity > 0 {
		return n >= f.minArity && n <= f.maxArity
	}
	return n == f.arity
}

var stdlibModules = map[string]map[string]stdlibFunc{
	"cache":  cacheFunctions,
	"data":   dataFunctions,
	"server": serverFunctions,
	"text": {
		"from":        {arity: 1, fn: stdTextFrom},
		"length":      {arity: 1, fn: stdTextLength},
		"upper":       {arity: 1, fn: stdTextUpper},
		"lower":       {arity: 1, fn: stdTextLower},
		"trim":        {arity: 1, fn: stdTextTrim},
		"contains":    {arity: 2, fn: stdTextContains},
		"starts_with": {arity: 2, fn: stdTextStartsWith},
		"ends_with":   {arity: 2, fn: stdTextEndsWith},
		"replace":     {arity: 3, fn: stdTextReplace},
		"split":       {arity: 2, fn: stdTextSplit},
		"join":        {arity: 2, fn: stdTextJoin},
		"to_integer":  {arity: 1, fn: stdTextToInteger},
		"to_decimal":  {arity: 1, fn: stdTextToDecimal},
	},
	"math": {
		"abs":   {arity: 1, fn: stdMathAbs},
		"min":   {arity: 2, fn: stdMathMin},
		"max":   {arity: 2, fn: stdMathMax},
		"clamp": {arity: 3, fn: stdMathClamp},
		"round": {arity: 2, fn: stdMathRound},
		"floor": {arity: 1, fn: stdMathFloor},
		"ceil":  {arity: 1, fn: stdMathCeil},
	},
	"collections": {
		"length": {arity: 1, fn: stdCollectionsLength},
		"append": {arity: 2, fn: stdCollectionsAppend},
		"slice":  {arity: 3, fn: stdCollectionsSlice},
		"keys":   {arity: 1, fn: stdCollectionsKeys},
		"put":    {arity: 3, fn: stdCollectionsPut},
		"remove": {arity: 2, fn: stdCollectionsRemove},
	},
	"dates": {
		"now_utc":      {arity: 0, fn: stdDatesNowUTC},
		"parse_iso":    {arity: 1, fn: stdDatesParseISO},
		"from_unix":    {arity: 1, fn: stdDatesFromUnix},
		"add_days":     {arity: 2, fn: stdDatesAddDays},
		"diff_seconds": {arity: 2, fn: stdDatesDiffSeconds},
	},
	"files": {
		"read_text":  {arity: 1, fn: stdFilesReadText},
		"write_text": {arity: 2, fn: stdFilesWriteText},
		"exists":     {arity: 1, fn: stdFilesExists},
		"list_dir":   {arity: 1, fn: stdFilesListDir},
	},
	"json": {
		"parse":            {arity: 1, fn: stdJSONParse},
		"stringify":        {arity: 1, fn: stdJSONStringify},
		"stringify_pretty": {arity: 1, fn: stdJSONStringifyPretty},
	},
	"http": {
		"get":      {minArity: 1, maxArity: 2, fn: stdHTTPGet},
		"get_json": {minArity: 1, maxArity: 2, fn: stdHTTPGetJSON},
		"post":     {minArity: 2, maxArity: 3, fn: stdHTTPPost},
		"put":      {minArity: 2, maxArity: 3, fn: stdHTTPPut},
		"patch":    {minArity: 2, maxArity: 3, fn: stdHTTPPatch},
		"delete":   {minArity: 1, maxArity: 2, fn: stdHTTPDelete},
		"request":  {arity: 1, fn: stdHTTPRequest},
	},
}

func isValidStdlibModule(name string) bool {
	_, ok := stdlibModules[name]
	return ok
}

func lookupStdlibFunc(module, name string) (stdlibFunc, bool) {
	mod, ok := stdlibModules[module]
	if !ok {
		return stdlibFunc{}, false
	}
	fn, ok := mod[name]
	return fn, ok
}

// ── text module ───────────────────────────────────────────────────────────────

func stdTextFrom(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].ContainsResource() {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	return grabrt.Text(args[0].Display()), nil
}

func stdTextLength(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	count := utf8.RuneCountInString(args[0].TextValue())
	return grabrt.IntegerFromInt64(int64(count)), nil
}

func stdTextUpper(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	return grabrt.Text(strings.ToUpper(args[0].TextValue())), nil
}

func stdTextLower(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	return grabrt.Text(strings.ToLower(args[0].TextValue())), nil
}

func stdTextTrim(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	return grabrt.Text(strings.TrimSpace(args[0].TextValue())), nil
}

func stdTextContains(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	return grabrt.Boolean(strings.Contains(args[0].TextValue(), args[1].TextValue())), nil
}

func stdTextStartsWith(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	return grabrt.Boolean(strings.HasPrefix(args[0].TextValue(), args[1].TextValue())), nil
}

func stdTextEndsWith(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	return grabrt.Boolean(strings.HasSuffix(args[0].TextValue(), args[1].TextValue())), nil
}

func stdTextReplace(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind || args[1].Kind() != grabrt.TextKind || args[2].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	return grabrt.Text(strings.ReplaceAll(args[0].TextValue(), args[1].TextValue(), args[2].TextValue())), nil
}

func stdTextSplit(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	parts := strings.Split(args[0].TextValue(), args[1].TextValue())
	values := make([]grabrt.Value, len(parts))
	for i, part := range parts {
		values[i] = grabrt.Text(part)
	}
	return grabrt.List(values), nil
}

func stdTextJoin(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.ListKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	items := args[0].ListValues()
	parts := make([]string, len(items))
	for i, item := range items {
		if item.Kind() != grabrt.TextKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
		}
		parts[i] = item.TextValue()
	}
	return grabrt.Text(strings.Join(parts, args[1].TextValue())), nil
}

func isValidIntegerText(s string) bool {
	if strings.HasPrefix(s, "-") {
		s = s[1:]
	}
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isValidDecimalText(s string) bool {
	if strings.HasPrefix(s, "-") {
		s = s[1:]
	}
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot >= len(s)-1 {
		return false
	}
	intPart := s[:dot]
	fracPart := s[dot+1:]
	return isValidIntegerText(intPart) && isValidIntegerText(fracPart) && !strings.HasPrefix(fracPart, "-")
}

func stdTextToInteger(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	raw := args[0].TextValue()
	if !isValidIntegerText(raw) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("cannot parse integer from %q", raw)}
	}
	val, err := grabrt.IntegerFromString(raw)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	return val, nil
}

func stdTextToDecimal(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	raw := args[0].TextValue()
	if !isValidDecimalText(raw) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("cannot parse decimal from %q", raw)}
	}
	val, err := grabrt.DecimalFromString(raw)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	return val, nil
}

// ── math module ───────────────────────────────────────────────────────────────

func isNumericKind(k grabrt.Kind) bool {
	return k == grabrt.IntegerKind || k == grabrt.DecimalKind || k == grabrt.FloatKind
}

func stdMathAbs(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	n := args[0]
	switch n.Kind() {
	case grabrt.IntegerKind:
		return grabrt.IntegerFromBig(new(big.Int).Abs(n.IntegerValue())), nil
	case grabrt.DecimalKind:
		d := n.DecimalValue()
		return grabrt.DecimalFromBig(new(big.Int).Abs(&d.Coefficient), d.Scale), nil
	case grabrt.FloatKind:
		val, err := grabrt.FloatFromFloat64(math.Abs(n.FloatValue()))
		if err != nil {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span, Err: err}
		}
		return val, nil
	default:
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
}

func stdMathMin(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	a, b := args[0], args[1]
	if !isNumericKind(a.Kind()) || !isNumericKind(b.Kind()) {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	cmp, ok := a.Compare(b)
	if !ok {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	if cmp <= 0 {
		return a, nil
	}
	return b, nil
}

func stdMathMax(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	a, b := args[0], args[1]
	if !isNumericKind(a.Kind()) || !isNumericKind(b.Kind()) {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	cmp, ok := a.Compare(b)
	if !ok {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	if cmp >= 0 {
		return a, nil
	}
	return b, nil
}

func stdMathClamp(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	val, low, high := args[0], args[1], args[2]
	if !isNumericKind(val.Kind()) || !isNumericKind(low.Kind()) || !isNumericKind(high.Kind()) {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	lowHighCmp, ok := low.Compare(high)
	if !ok {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	if lowHighCmp > 0 {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("math.clamp requires low <= high")}
	}
	cmpLow, ok1 := val.Compare(low)
	cmpHigh, ok2 := val.Compare(high)
	if !ok1 || !ok2 {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	if cmpLow < 0 {
		return low, nil
	}
	if cmpHigh > 0 {
		return high, nil
	}
	return val, nil
}

func pow10Big(exp int) *big.Int {
	if exp <= 0 {
		return big.NewInt(1)
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp)), nil)
}

func stdMathRound(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	n, scaleVal := args[0], args[1]
	if (n.Kind() != grabrt.IntegerKind && n.Kind() != grabrt.DecimalKind) || scaleVal.Kind() != grabrt.IntegerKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	bigScale := scaleVal.IntegerValue()
	if bigScale.Sign() < 0 || !bigScale.IsInt64() || bigScale.Int64() > 16 {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("math.round scale must be between 0 and 16")}
	}
	targetScale := int(bigScale.Int64())

	var coeff *big.Int
	var curScale int
	if n.Kind() == grabrt.IntegerKind {
		coeff = n.IntegerValue()
		curScale = 0
	} else {
		d := n.DecimalValue()
		coeff = new(big.Int).Set(&d.Coefficient)
		curScale = d.Scale
	}

	if targetScale >= curScale {
		if targetScale > curScale {
			coeff.Mul(coeff, pow10Big(targetScale-curScale))
		}
		if targetScale == 0 {
			return grabrt.IntegerFromBig(coeff), nil
		}
		return grabrt.DecimalFromBig(coeff, targetScale), nil
	}

	divisor := pow10Big(curScale - targetScale)
	absCoeff := new(big.Int).Abs(coeff)
	quo, rem := new(big.Int).QuoRem(absCoeff, divisor, new(big.Int))
	if rem.Sign() > 0 {
		twiceRem := new(big.Int).Mul(rem, big.NewInt(2))
		cmp := twiceRem.Cmp(divisor)
		if cmp > 0 || (cmp == 0 && quo.Bit(0) == 1) {
			quo.Add(quo, big.NewInt(1))
		}
	}
	if coeff.Sign() < 0 && quo.Sign() != 0 {
		quo.Neg(quo)
	}
	if targetScale == 0 {
		return grabrt.IntegerFromBig(quo), nil
	}
	return grabrt.DecimalFromBig(quo, targetScale), nil
}

func stdMathFloor(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	n := args[0]
	switch n.Kind() {
	case grabrt.IntegerKind:
		return n, nil
	case grabrt.DecimalKind:
		d := n.DecimalValue()
		if d.Scale <= 0 {
			return grabrt.IntegerFromBig(&d.Coefficient), nil
		}
		divisor := pow10Big(d.Scale)
		quo, rem := new(big.Int).QuoRem(&d.Coefficient, divisor, new(big.Int))
		if rem.Sign() < 0 {
			quo.Sub(quo, big.NewInt(1))
		}
		return grabrt.IntegerFromBig(quo), nil
	case grabrt.FloatKind:
		f := math.Floor(n.FloatValue())
		bf, _ := new(big.Float).SetFloat64(f).Int(nil)
		return grabrt.IntegerFromBig(bf), nil
	default:
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
}

func stdMathCeil(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	n := args[0]
	switch n.Kind() {
	case grabrt.IntegerKind:
		return n, nil
	case grabrt.DecimalKind:
		d := n.DecimalValue()
		if d.Scale <= 0 {
			return grabrt.IntegerFromBig(&d.Coefficient), nil
		}
		divisor := pow10Big(d.Scale)
		quo, rem := new(big.Int).QuoRem(&d.Coefficient, divisor, new(big.Int))
		if rem.Sign() > 0 {
			quo.Add(quo, big.NewInt(1))
		}
		return grabrt.IntegerFromBig(quo), nil
	case grabrt.FloatKind:
		f := math.Ceil(n.FloatValue())
		bf, _ := new(big.Float).SetFloat64(f).Int(nil)
		return grabrt.IntegerFromBig(bf), nil
	default:
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
}

// ── collections module ────────────────────────────────────────────────────────

func stdCollectionsLength(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	coll := args[0]
	switch coll.Kind() {
	case grabrt.ListKind:
		return grabrt.IntegerFromInt64(int64(coll.ListLen())), nil
	case grabrt.MapKind, grabrt.RecordKind:
		return grabrt.IntegerFromInt64(int64(len(coll.Entries()))), nil
	case grabrt.BytesKind:
		return grabrt.IntegerFromInt64(int64(len(coll.BytesValue()))), nil
	default:
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
}

func stdCollectionsAppend(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.ListKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	items := append(args[0].ListValues(), args[1])
	return grabrt.List(items), nil
}

func stdCollectionsSlice(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.ListKind || args[1].Kind() != grabrt.IntegerKind || args[2].Kind() != grabrt.IntegerKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	items := args[0].ListValues()
	startBig := args[1].IntegerValue()
	endBig := args[2].IntegerValue()
	if startBig.Sign() < 0 || !startBig.IsInt64() || endBig.Sign() < 0 || !endBig.IsInt64() {
		return grabrt.Value{}, &Error{Kind: IndexOutOfBounds, Span: span}
	}
	start := int(startBig.Int64())
	end := int(endBig.Int64())
	if start < 0 || end < start || end > len(items) {
		return grabrt.Value{}, &Error{Kind: IndexOutOfBounds, Span: span}
	}
	return grabrt.List(items[start:end]), nil
}

func stdCollectionsKeys(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	coll := args[0]
	if coll.Kind() != grabrt.MapKind && coll.Kind() != grabrt.RecordKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	entries := coll.Entries()
	keys := make([]grabrt.Value, len(entries))
	for i, entry := range entries {
		keys[i] = grabrt.Text(entry.Name)
	}
	return grabrt.List(keys), nil
}

func stdCollectionsPut(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.MapKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	key := args[1].TextValue()
	val := args[2]
	entries := args[0].Entries()
	updated := false
	for i := range entries {
		if entries[i].Name == key {
			entries[i].Value = val
			updated = true
			break
		}
	}
	if !updated {
		entries = append(entries, grabrt.Entry{Name: key, Value: val})
	}
	return grabrt.Map(entries), nil
}

func stdCollectionsRemove(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.MapKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	key := args[1].TextValue()
	entries := args[0].Entries()
	out := make([]grabrt.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name != key {
			out = append(out, entry)
		}
	}
	return grabrt.Map(out), nil
}

// ── dates module ──────────────────────────────────────────────────────────────

func timeToDateRecord(t time.Time) grabrt.Value {
	u := t.UTC()
	return grabrt.Record([]grabrt.Entry{
		{Name: "iso", Value: grabrt.Text(u.Format("2006-01-02T15:04:05Z"))},
		{Name: "year", Value: grabrt.IntegerFromInt64(int64(u.Year()))},
		{Name: "month", Value: grabrt.IntegerFromInt64(int64(u.Month()))},
		{Name: "day", Value: grabrt.IntegerFromInt64(int64(u.Day()))},
		{Name: "hour", Value: grabrt.IntegerFromInt64(int64(u.Hour()))},
		{Name: "minute", Value: grabrt.IntegerFromInt64(int64(u.Minute()))},
		{Name: "second", Value: grabrt.IntegerFromInt64(int64(u.Second()))},
		{Name: "unix", Value: grabrt.IntegerFromInt64(u.Unix())},
	})
}

func extractDateUnix(val grabrt.Value, span lexer.Span) (int64, error) {
	if val.Kind() != grabrt.RecordKind {
		return 0, &Error{Kind: TypeMismatch, Span: span}
	}
	unixVal, ok := val.LookupEntry("unix")
	if !ok || unixVal.Kind() != grabrt.IntegerKind || !unixVal.IntegerValue().IsInt64() {
		return 0, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("invalid date record")}
	}
	return unixVal.IntegerValue().Int64(), nil
}

func stdDatesNowUTC(_ *execContext, _ lexer.Span, _ []grabrt.Value) (grabrt.Value, error) {
	return timeToDateRecord(time.Now().UTC()), nil
}

func stdDatesParseISO(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	raw := args[0].TextValue()
	if t, err := time.Parse("2006-01-02T15:04:05Z", raw); err == nil {
		return timeToDateRecord(t.UTC()), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return timeToDateRecord(t.UTC()), nil
	}
	return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("invalid ISO date or timestamp %q", raw)}
}

func stdDatesFromUnix(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.IntegerKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	secBig := args[0].IntegerValue()
	if !secBig.IsInt64() {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("unix timestamp out of range")}
	}
	return timeToDateRecord(time.Unix(secBig.Int64(), 0).UTC()), nil
}

func stdDatesAddDays(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[1].Kind() != grabrt.IntegerKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	unixSec, err := extractDateUnix(args[0], span)
	if err != nil {
		return grabrt.Value{}, err
	}
	daysBig := args[1].IntegerValue()
	if !daysBig.IsInt64() {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("days offset out of range")}
	}
	t := time.Unix(unixSec, 0).UTC().AddDate(0, 0, int(daysBig.Int64()))
	return timeToDateRecord(t), nil
}

func stdDatesDiffSeconds(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	unixA, err := extractDateUnix(args[0], span)
	if err != nil {
		return grabrt.Value{}, err
	}
	unixB, err := extractDateUnix(args[1], span)
	if err != nil {
		return grabrt.Value{}, err
	}
	return grabrt.IntegerFromInt64(unixA - unixB), nil
}

// ── files module ──────────────────────────────────────────────────────────────

func stdFilesReadText(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	resolved, err := ctx.permissions.resolveReadPath(args[0].TextValue())
	if err != nil {
		return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: err}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsPermission(err) {
			return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: err}
		}
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	if !info.Mode().IsRegular() {
		return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: fmt.Errorf("not a regular file")}
	}
	if info.Size() > maxFileSizeBytes {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("file exceeds 10 MiB limit")}
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	if len(data) > maxFileSizeBytes {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("file exceeds 10 MiB limit")}
	}
	if !utf8.Valid(data) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("file is not valid UTF-8")}
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	return grabrt.Text(normalized), nil
}

func stdFilesWriteText(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	content := args[1].TextValue()
	if len(content) > maxFileSizeBytes || !utf8.ValidString(content) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("invalid or oversized content")}
	}
	resolved, err := ctx.permissions.resolveWritePath(args[0].TextValue())
	if err != nil {
		return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: err}
	}
	if info, err := os.Lstat(resolved); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: fmt.Errorf("target is not a regular file")}
		}
	}
	if err := os.WriteFile(resolved, []byte(content), 0o600); err != nil {
		if os.IsPermission(err) {
			return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: err}
		}
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	return grabrt.Nothing(), nil
}

func stdFilesExists(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	resolved, err := ctx.permissions.resolveReadPath(args[0].TextValue())
	if err != nil {
		return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: err}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return grabrt.Boolean(false), nil
		}
		if os.IsPermission(err) {
			return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: err}
		}
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	return grabrt.Boolean(info.Mode().IsRegular() || info.IsDir()), nil
}

func stdFilesListDir(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	resolved, err := ctx.permissions.resolveReadPath(args[0].TextValue())
	if err != nil {
		return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: err}
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		if os.IsPermission(err) {
			return grabrt.Value{}, &Error{Kind: PermissionDenied, Span: span, Err: err}
		}
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if isSensitiveSegment(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]grabrt.Value, len(names))
	for i, name := range names {
		values[i] = grabrt.Text(name)
	}
	return grabrt.List(values), nil
}
