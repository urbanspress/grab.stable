package lexer

import (
	"fmt"
	"unicode/utf8"
)

// Position identifies a point in source text. Offset is a zero-based byte
// offset; Line and Column are one-based, with columns counting Unicode scalar
// values.
type Position struct {
	Offset int
	Line   int
	Column int
}

// Span is a half-open range in source text.
type Span struct {
	Start Position
	End   Position
}

// InvalidUTF8Error reports the first invalid byte in a source file.
type InvalidUTF8Error struct {
	Span Span
}

func (e *InvalidUTF8Error) Error() string {
	return fmt.Sprintf("invalid UTF-8 at line %d, column %d", e.Span.Start.Line, e.Span.Start.Column)
}

// Code returns the stable GRAB 0.4 error code for invalid source encoding.
func (e *InvalidUTF8Error) Code() string {
	return "GRAB-E001"
}

// SourceReader reads Unicode scalar values while retaining byte offsets and
// one-based line and column positions.
type SourceReader struct {
	name   string
	source string
	offset int
	line   int
	column int
}

// NewSourceReader creates a reader for valid UTF-8 source. CRLF is returned as
// one logical newline; LF is also returned as one newline. A lone CR is left
// unchanged so the lexer can report it as an invalid source character.
func NewSourceReader(name, source string) (*SourceReader, error) {
	if !utf8.ValidString(source) {
		return nil, invalidUTF8Error(source)
	}

	return &SourceReader{
		name:   name,
		source: source,
		line:   1,
		column: 1,
	}, nil
}

// Name returns the source name supplied to NewSourceReader.
func (r *SourceReader) Name() string {
	return r.name
}

// Position returns the reader's current position, which is also the position
// at which the next rune will be read.
func (r *SourceReader) Position() Position {
	return Position{Offset: r.offset, Line: r.line, Column: r.column}
}

// PeekRune returns the next rune without advancing the reader. CRLF is
// normalized to '\n'.
func (r *SourceReader) PeekRune() (rune, bool) {
	if r.offset >= len(r.source) {
		return 0, false
	}

	value, size := utf8.DecodeRuneInString(r.source[r.offset:])
	if value == '\r' && r.offset+size < len(r.source) && r.source[r.offset+size] == '\n' {
		return '\n', true
	}
	return value, true
}

// ReadRune returns the next rune and its source span. CRLF is returned as one
// logical newline whose byte span covers both source bytes. At end of input,
// it returns ok=false and does not advance.
func (r *SourceReader) ReadRune() (value rune, span Span, ok bool) {
	if r.offset >= len(r.source) {
		return 0, Span{}, false
	}

	start := r.Position()
	value, size := utf8.DecodeRuneInString(r.source[r.offset:])
	if value == '\r' && r.offset+size < len(r.source) && r.source[r.offset+size] == '\n' {
		value = '\n'
		size++
	}

	r.offset += size
	if value == '\n' {
		r.line++
		r.column = 1
	} else {
		r.column++
	}

	return value, Span{Start: start, End: r.Position()}, true
}

// Slice returns the source text covered by span. It returns false if the byte
// offsets are invalid or do not fall on UTF-8 rune boundaries.
func (r *SourceReader) Slice(span Span) (string, bool) {
	start, end := span.Start.Offset, span.End.Offset
	if start < 0 || end < start || end > len(r.source) {
		return "", false
	}
	if !isRuneBoundary(r.source, start) || !isRuneBoundary(r.source, end) {
		return "", false
	}
	return r.source[start:end], true
}

func invalidUTF8Error(source string) *InvalidUTF8Error {
	line, column := 1, 1
	for offset := 0; offset < len(source); {
		value, size := utf8.DecodeRuneInString(source[offset:])
		if value == utf8.RuneError && size == 1 {
			start := Position{Offset: offset, Line: line, Column: column}
			end := Position{Offset: offset + 1, Line: line, Column: column + 1}
			return &InvalidUTF8Error{Span: Span{Start: start, End: end}}
		}

		if value == '\r' && offset+size < len(source) && source[offset+size] == '\n' {
			value = '\n'
			size++
		}
		offset += size
		if value == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return nil
}

func isRuneBoundary(source string, offset int) bool {
	return offset == len(source) || utf8.RuneStart(source[offset])
}
