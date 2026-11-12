package template

import (
	"fmt"
	"strings"

	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

// ErrorKind classifies template lexical, syntax, context, binding, and security errors.
type ErrorKind uint8

const (
	InvalidEncoding ErrorKind = iota
	MalformedInterpolation
	DisallowedContext
	UnboundVariable
	UnsafeURLScheme
)

// String returns the stable specification category name for the template error.
func (kind ErrorKind) String() string {
	switch kind {
	case InvalidEncoding:
		return "invalid template encoding"
	case MalformedInterpolation:
		return "malformed template interpolation"
	case DisallowedContext:
		return "disallowed interpolation context"
	case UnboundVariable:
		return "unbound template variable"
	case UnsafeURLScheme:
		return "unsafe URL scheme"
	default:
		return "unknown template error"
	}
}

// Error carries the source span and stable category for a GRAB 0.2 template diagnostic.
type Error struct {
	Kind ErrorKind
	Name string
	Span lexer.Span
}

func (e *Error) Error() string {
	if e.Name != "" && (e.Kind == UnboundVariable || e.Kind == UnsafeURLScheme) {
		return fmt.Sprintf("%s %q at line %d, column %d", e.Kind, e.Name, e.Span.Start.Line, e.Span.Start.Column)
	}
	return fmt.Sprintf("%s at line %d, column %d", e.Kind, e.Span.Start.Line, e.Span.Start.Column)
}

// Context identifies the validated HTML lexical context of an interpolation tag.
type Context uint8

const (
	ContextText Context = iota
	ContextNormalAttr
	ContextURLAttr
)

type segmentKind uint8

const (
	segmentStatic segmentKind = iota
	segmentInterpolation
)

type segment struct {
	kind         segmentKind
	text         string
	name         string
	tagSpan      lexer.Span
	nameSpan     lexer.Span
	context      Context
	isLeadingURL bool
}

// Template is a parsed and context-validated .grab.html template.
type Template struct {
	name     string
	segments []segment
}

type htmlState uint8

const (
	stateData htmlState = iota
	stateTagOpen
	stateEndTagOpen
	stateEndTagName
	stateMarkupDeclOpen
	stateComment
	stateDocType
	stateBogusComment
	stateTagName
	stateSelfClosingStartTag
	stateBeforeAttrName
	stateAttrName
	stateAfterAttrName
	stateBeforeAttrValue
	stateAttrValueDoubleQuoted
	stateAttrValueSingleQuoted
	stateAttrValueUnquoted
	stateAfterAttrValueQuoted
	stateRawTextScript
	stateRawTextStyle
)

// Parse validates UTF-8 encoding, parses {<identifier>} interpolations and
// escaped braces ({{ and }}), and verifies that every interpolation occurs in a
// permitted HTML context.
func Parse(name, source string) (*Template, error) {
	reader, err := lexer.NewSourceReader(name, source)
	if err != nil {
		if utf8Err, ok := err.(*lexer.InvalidUTF8Error); ok {
			return nil, &Error{Kind: InvalidEncoding, Span: utf8Err.Span}
		}
		return nil, err
	}

	var segments []segment
	var staticBuf strings.Builder
	flushStatic := func() {
		if staticBuf.Len() > 0 {
			segments = append(segments, segment{
				kind: segmentStatic,
				text: staticBuf.String(),
			})
			staticBuf.Reset()
		}
	}

	state := stateData
	var currentTagName strings.Builder
	var currentAttrName strings.Builder
	var rawTextBuf strings.Builder
	attrHasNonSpace := false
	var unclosedTagInterp *lexer.Span

	stepHTMLChar := func(ch rune) {
		switch state {
		case stateData:
			if ch == '<' {
				state = stateTagOpen
				unclosedTagInterp = nil
			}
		case stateTagOpen:
			switch {
			case ch == '!':
				state = stateMarkupDeclOpen
				rawTextBuf.Reset()
			case ch == '?':
				state = stateBogusComment
			case ch == '/':
				state = stateEndTagOpen
			case isASCIILetter(ch):
				currentTagName.Reset()
				currentTagName.WriteRune(toLowerASCII(ch))
				state = stateTagName
			default:
				state = stateData
			}
		case stateEndTagOpen:
			if ch == '>' {
				state = stateData
				unclosedTagInterp = nil
			} else if isASCIILetter(ch) {
				state = stateEndTagName
			} else {
				state = stateBogusComment
			}
		case stateEndTagName:
			if ch == '>' {
				state = stateData
				unclosedTagInterp = nil
			}
		case stateMarkupDeclOpen:
			rawTextBuf.WriteRune(ch)
			bufStr := rawTextBuf.String()
			if bufStr == "--" {
				state = stateComment
				rawTextBuf.Reset()
			} else if ch == '>' {
				state = stateData
				unclosedTagInterp = nil
			} else if len(bufStr) >= 2 && !strings.HasPrefix("--", bufStr) {
				state = stateDocType
			}
		case stateComment:
			rawTextBuf.WriteRune(ch)
			if strings.HasSuffix(rawTextBuf.String(), "-->") {
				state = stateData
				rawTextBuf.Reset()
				unclosedTagInterp = nil
			}
		case stateDocType, stateBogusComment:
			if ch == '>' {
				state = stateData
				unclosedTagInterp = nil
			}
		case stateTagName:
			switch {
			case isHTMLSpace(ch):
				state = stateBeforeAttrName
			case ch == '/':
				state = stateSelfClosingStartTag
			case ch == '>':
				state = enterElementBody(currentTagName.String())
				rawTextBuf.Reset()
				unclosedTagInterp = nil
			default:
				currentTagName.WriteRune(toLowerASCII(ch))
			}
		case stateSelfClosingStartTag:
			if ch == '>' {
				state = stateData
				unclosedTagInterp = nil
			} else {
				state = stateBeforeAttrName
			}
		case stateBeforeAttrName:
			switch {
			case isHTMLSpace(ch):
				// remain
			case ch == '/':
				state = stateSelfClosingStartTag
			case ch == '>':
				state = enterElementBody(currentTagName.String())
				rawTextBuf.Reset()
				unclosedTagInterp = nil
			default:
				currentAttrName.Reset()
				currentAttrName.WriteRune(toLowerASCII(ch))
				state = stateAttrName
			}
		case stateAttrName:
			switch {
			case isHTMLSpace(ch):
				state = stateAfterAttrName
			case ch == '=':
				state = stateBeforeAttrValue
			case ch == '/':
				state = stateSelfClosingStartTag
			case ch == '>':
				state = enterElementBody(currentTagName.String())
				rawTextBuf.Reset()
				unclosedTagInterp = nil
			default:
				currentAttrName.WriteRune(toLowerASCII(ch))
			}
		case stateAfterAttrName:
			switch {
			case isHTMLSpace(ch):
				// remain
			case ch == '=':
				state = stateBeforeAttrValue
			case ch == '/':
				state = stateSelfClosingStartTag
			case ch == '>':
				state = enterElementBody(currentTagName.String())
				rawTextBuf.Reset()
				unclosedTagInterp = nil
			default:
				currentAttrName.Reset()
				currentAttrName.WriteRune(toLowerASCII(ch))
				state = stateAttrName
			}
		case stateBeforeAttrValue:
			switch {
			case isHTMLSpace(ch):
				// remain
			case ch == '"':
				attrHasNonSpace = false
				state = stateAttrValueDoubleQuoted
			case ch == '\'':
				attrHasNonSpace = false
				state = stateAttrValueSingleQuoted
			case ch == '>':
				state = enterElementBody(currentTagName.String())
				rawTextBuf.Reset()
				unclosedTagInterp = nil
			default:
				state = stateAttrValueUnquoted
			}
		case stateAttrValueDoubleQuoted:
			if ch == '"' {
				state = stateAfterAttrValueQuoted
			} else if !isHTMLSpace(ch) {
				attrHasNonSpace = true
			}
		case stateAttrValueSingleQuoted:
			if ch == '\'' {
				state = stateAfterAttrValueQuoted
			} else if !isHTMLSpace(ch) {
				attrHasNonSpace = true
			}
		case stateAttrValueUnquoted:
			if isHTMLSpace(ch) {
				state = stateBeforeAttrName
			} else if ch == '>' {
				state = enterElementBody(currentTagName.String())
				rawTextBuf.Reset()
				unclosedTagInterp = nil
			}
		case stateAfterAttrValueQuoted:
			switch {
			case isHTMLSpace(ch):
				state = stateBeforeAttrName
			case ch == '/':
				state = stateSelfClosingStartTag
			case ch == '>':
				state = enterElementBody(currentTagName.String())
				rawTextBuf.Reset()
				unclosedTagInterp = nil
			default:
				currentAttrName.Reset()
				currentAttrName.WriteRune(toLowerASCII(ch))
				state = stateAttrName
			}
		case stateRawTextScript:
			rawTextBuf.WriteRune(toLowerASCII(ch))
			if ch == '>' && hasClosingRawTag(rawTextBuf.String(), "script") {
				state = stateData
				rawTextBuf.Reset()
			}
		case stateRawTextStyle:
			rawTextBuf.WriteRune(toLowerASCII(ch))
			if ch == '>' && hasClosingRawTag(rawTextBuf.String(), "style") {
				state = stateData
				rawTextBuf.Reset()
			}
		}
	}

	for {
		ch, span, ok := reader.ReadRune()
		if !ok {
			break
		}

		if ch == '\r' {
			ch = '\n'
		}

		if ch == '}' {
			if next, ok := reader.PeekRune(); ok && next == '}' {
				reader.ReadRune()
				staticBuf.WriteByte('}')
				stepHTMLChar('}')
				continue
			}
			return nil, &Error{Kind: MalformedInterpolation, Span: span}
		}

		if ch == '{' {
			if next, ok := reader.PeekRune(); ok && next == '{' {
				reader.ReadRune()
				staticBuf.WriteByte('{')
				stepHTMLChar('{')
				continue
			}

			identName, identSpan, tagSpan, err := scanInterpolationTag(reader, span)
			if err != nil {
				return nil, err
			}

			ctx, isLeadingURL, ok := validateInterpolationContext(state, currentAttrName.String(), attrHasNonSpace)
			if !ok {
				return nil, &Error{Kind: DisallowedContext, Span: tagSpan}
			}
			if state == stateAttrValueDoubleQuoted || state == stateAttrValueSingleQuoted {
				attrHasNonSpace = true
				if unclosedTagInterp == nil {
					spanCopy := tagSpan
					unclosedTagInterp = &spanCopy
				}
			}

			flushStatic()
			segments = append(segments, segment{
				kind:         segmentInterpolation,
				name:         identName,
				tagSpan:      tagSpan,
				nameSpan:     identSpan,
				context:      ctx,
				isLeadingURL: isLeadingURL,
			})
			continue
		}

		staticBuf.WriteRune(ch)
		stepHTMLChar(ch)
	}

	if unclosedTagInterp != nil {
		return nil, &Error{Kind: DisallowedContext, Span: *unclosedTagInterp}
	}

	flushStatic()
	return &Template{name: name, segments: segments}, nil
}

func scanInterpolationTag(reader *lexer.SourceReader, openSpan lexer.Span) (string, lexer.Span, lexer.Span, error) {
	// Skip optional horizontal whitespace after '{'.
	for {
		peek, ok := reader.PeekRune()
		if !ok || peek == '\n' || peek == '\r' {
			return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
		}
		if peek == ' ' || peek == '\t' {
			reader.ReadRune()
			continue
		}
		break
	}

	first, firstSpan, ok := reader.ReadRune()
	if !ok || first == '\n' || first == '\r' {
		return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
	}
	if first == '}' {
		// Empty "{}" or "{   }".
		fullSpan := lexer.Span{Start: openSpan.Start, End: firstSpan.End}
		return "", lexer.Span{}, fullSpan, &Error{Kind: MalformedInterpolation, Span: fullSpan}
	}
	if first == '{' {
		return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: firstSpan}
	}
	if !isASCIIIdentStart(first) {
		endPos := firstSpan.End
		for {
			peek, ok := reader.PeekRune()
			if !ok || peek == '\n' || peek == '\r' {
				return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
			}
			if peek == '}' || peek == ' ' || peek == '\t' {
				break
			}
			_, sp, _ := reader.ReadRune()
			endPos = sp.End
		}
		// Ensure closing '}' exists; otherwise report unclosed '{'.
		if !consumeToClosingBrace(reader) {
			return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
		}
		badSpan := lexer.Span{Start: firstSpan.Start, End: endPos}
		return "", lexer.Span{}, badSpan, &Error{Kind: MalformedInterpolation, Span: badSpan}
	}

	identEnd := firstSpan.End
	for {
		peek, ok := reader.PeekRune()
		if !ok || peek == '\n' || peek == '\r' {
			return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
		}
		if !isASCIIIdentContinue(peek) {
			break
		}
		_, sp, _ := reader.ReadRune()
		identEnd = sp.End
	}
	identSpan := lexer.Span{Start: firstSpan.Start, End: identEnd}
	identName, _ := reader.Slice(identSpan)

	if isReservedKeyword(identName) {
		if !consumeToClosingBrace(reader) {
			return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
		}
		return "", lexer.Span{}, identSpan, &Error{Kind: MalformedInterpolation, Span: identSpan}
	}

	// Skip optional horizontal whitespace after identifier.
	for {
		peek, ok := reader.PeekRune()
		if !ok || peek == '\n' || peek == '\r' {
			return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
		}
		if peek == ' ' || peek == '\t' {
			reader.ReadRune()
			continue
		}
		break
	}

	closeCh, closeSpan, ok := reader.ReadRune()
	if !ok || closeCh == '\n' || closeCh == '\r' {
		return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
	}
	if closeCh != '}' {
		extraEnd := closeSpan.End
		for {
			peek, ok := reader.PeekRune()
			if !ok || peek == '\n' || peek == '\r' {
				return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
			}
			if peek == '}' || peek == ' ' || peek == '\t' {
				break
			}
			_, sp, _ := reader.ReadRune()
			extraEnd = sp.End
		}
		if !consumeToClosingBrace(reader) {
			return "", lexer.Span{}, lexer.Span{}, &Error{Kind: MalformedInterpolation, Span: openSpan}
		}
		extraSpan := lexer.Span{Start: closeSpan.Start, End: extraEnd}
		return "", lexer.Span{}, extraSpan, &Error{Kind: MalformedInterpolation, Span: extraSpan}
	}

	tagSpan := lexer.Span{Start: openSpan.Start, End: closeSpan.End}
	return identName, identSpan, tagSpan, nil
}

func consumeToClosingBrace(reader *lexer.SourceReader) bool {
	for {
		ch, _, ok := reader.ReadRune()
		if !ok || ch == '\n' || ch == '\r' || ch == '{' {
			return false
		}
		if ch == '}' {
			return true
		}
	}
}

func validateInterpolationContext(state htmlState, attrName string, attrHasNonSpace bool) (Context, bool, bool) {
	switch state {
	case stateData:
		return ContextText, false, true
	case stateAttrValueDoubleQuoted, stateAttrValueSingleQuoted:
		if isDisallowedAttr(attrName) {
			return ContextNormalAttr, false, false
		}
		if isURLAttr(attrName) {
			return ContextURLAttr, !attrHasNonSpace, true
		}
		return ContextNormalAttr, false, true
	default:
		return ContextText, false, false
	}
}

func enterElementBody(tagName string) htmlState {
	switch tagName {
	case "script":
		return stateRawTextScript
	case "style":
		return stateRawTextStyle
	default:
		return stateData
	}
}

func hasClosingRawTag(buf, tagName string) bool {
	target := "</" + tagName
	idx := strings.LastIndex(buf, target)
	if idx < 0 {
		return false
	}
	rest := buf[idx+len(target):]
	if rest == ">" {
		return true
	}
	for i := 0; i < len(rest)-1; i++ {
		if !isHTMLSpace(rune(rest[i])) && rest[i] != '/' {
			return false
		}
	}
	return strings.HasSuffix(rest, ">")
}

func isDisallowedAttr(attrName string) bool {
	if strings.HasPrefix(attrName, "on") && len(attrName) >= 3 {
		return true
	}
	return attrName == "style" || attrName == "srcdoc"
}

func isURLAttr(attrName string) bool {
	switch attrName {
	case "href", "src", "action", "formaction":
		return true
	default:
		return false
	}
}

// ValidateBindings checks that every {<identifier>} referenced by the template
// exists in the provided set of bound variable names.
func ValidateBindings(tmpl *Template, bound map[string]struct{}) error {
	for _, seg := range tmpl.segments {
		if seg.kind != segmentInterpolation {
			continue
		}
		if _, ok := bound[seg.name]; !ok {
			return &Error{
				Kind: UnboundVariable,
				Name: seg.name,
				Span: seg.nameSpan,
			}
		}
	}
	return nil
}

// Render verifies that all template variables are bound in env, applies
// canonical GRAB 0.1 display formatting and context-aware HTML escaping, and
// returns the complete rendered HTML string. If any error occurs, no partial
// output is returned.
func Render(tmpl *Template, env map[string]grabrt.Value) (string, error) {
	for _, seg := range tmpl.segments {
		if seg.kind != segmentInterpolation {
			continue
		}
		if _, ok := env[seg.name]; !ok {
			return "", &Error{
				Kind: UnboundVariable,
				Name: seg.name,
				Span: seg.nameSpan,
			}
		}
	}

	var out strings.Builder
	for _, seg := range tmpl.segments {
		if seg.kind == segmentStatic {
			out.WriteString(seg.text)
			continue
		}
		val := env[seg.name]
		if val.ContainsResource() {
			return "", fmt.Errorf("host resources cannot be rendered in templates")
		}
		raw := val.Display()
		if seg.context == ContextURLAttr && seg.isLeadingURL {
			if !isSafeURL(raw) {
				return "", &Error{
					Kind: UnsafeURLScheme,
					Name: seg.name,
					Span: seg.tagSpan,
				}
			}
		}
		out.WriteString(escapeHTML(raw))
	}
	return out.String(), nil
}

func isSafeURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	// Find whether a ':' precedes any '/', '?', or '#'.
	colonIdx := -1
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if ch == '/' || ch == '?' || ch == '#' {
			break
		}
		if ch == ':' {
			colonIdx = i
			break
		}
	}
	if colonIdx < 0 {
		// Relative URL without scheme.
		return true
	}
	scheme := strings.ToLower(trimmed[:colonIdx])
	rest := trimmed[colonIdx+1:]
	switch scheme {
	case "http", "https":
		return strings.HasPrefix(rest, "//")
	case "mailto":
		return true
	default:
		return false
	}
}

func escapeHTML(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&#34;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIIIdentStart(r rune) bool {
	return r == '_' || isASCIILetter(r)
}

func isASCIIIdentContinue(r rune) bool {
	return isASCIIIdentStart(r) || (r >= '0' && r <= '9')
}

func toLowerASCII(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

func isHTMLSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

func isReservedKeyword(ident string) bool {
	switch ident {
	case "set", "to", "show", "joined", "with", "yes", "no", "nothing", "record":
		return true
	default:
		return false
	}
}
