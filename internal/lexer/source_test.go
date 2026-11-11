package lexer

import (
	"errors"
	"testing"
)

func TestSourceReaderReadRune(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []struct {
			value rune
			span  Span
		}
		end Position
	}{
		{
			name:  "empty source",
			input: "",
			end:   Position{Offset: 0, Line: 1, Column: 1},
		},
		{
			name:  "unicode and CRLF",
			input: "a\té\r\nB\n",
			want: []struct {
				value rune
				span  Span
			}{
				{value: 'a', span: Span{Start: Position{Offset: 0, Line: 1, Column: 1}, End: Position{Offset: 1, Line: 1, Column: 2}}},
				{value: '\t', span: Span{Start: Position{Offset: 1, Line: 1, Column: 2}, End: Position{Offset: 2, Line: 1, Column: 3}}},
				{value: 'é', span: Span{Start: Position{Offset: 2, Line: 1, Column: 3}, End: Position{Offset: 4, Line: 1, Column: 4}}},
				{value: '\n', span: Span{Start: Position{Offset: 4, Line: 1, Column: 4}, End: Position{Offset: 6, Line: 2, Column: 1}}},
				{value: 'B', span: Span{Start: Position{Offset: 6, Line: 2, Column: 1}, End: Position{Offset: 7, Line: 2, Column: 2}}},
				{value: '\n', span: Span{Start: Position{Offset: 7, Line: 2, Column: 2}, End: Position{Offset: 8, Line: 3, Column: 1}}},
			},
			end: Position{Offset: 8, Line: 3, Column: 1},
		},
		{
			name:  "lone carriage return remains visible",
			input: "\r",
			want: []struct {
				value rune
				span  Span
			}{
				{value: '\r', span: Span{Start: Position{Offset: 0, Line: 1, Column: 1}, End: Position{Offset: 1, Line: 1, Column: 2}}},
			},
			end: Position{Offset: 1, Line: 1, Column: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := NewSourceReader("test.grab", tt.input)
			if err != nil {
				t.Fatalf("NewSourceReader() error = %v", err)
			}
			if got := reader.Name(); got != "test.grab" {
				t.Fatalf("Name() = %q, want %q", got, "test.grab")
			}

			for i, want := range tt.want {
				peeked, ok := reader.PeekRune()
				if !ok || peeked != want.value {
					t.Fatalf("PeekRune() = (%q, %t), want (%q, true)", peeked, ok, want.value)
				}
				if got := reader.Position(); got != want.span.Start {
					t.Fatalf("Position() before rune %d = %+v, want %+v", i, got, want.span.Start)
				}

				gotValue, gotSpan, ok := reader.ReadRune()
				if !ok || gotValue != want.value || gotSpan != want.span {
					t.Fatalf("ReadRune() = (%q, %+v, %t), want (%q, %+v, true)", gotValue, gotSpan, ok, want.value, want.span)
				}
				if gotText, ok := reader.Slice(gotSpan); !ok || gotText == "" {
					t.Fatalf("Slice(%+v) = (%q, %t), want a non-empty source slice", gotSpan, gotText, ok)
				}
			}

			if _, _, ok := reader.ReadRune(); ok {
				t.Fatal("ReadRune() at EOF succeeded, want ok=false")
			}
			if got := reader.Position(); got != tt.end {
				t.Fatalf("Position() at EOF = %+v, want %+v", got, tt.end)
			}
		})
	}
}

func TestNewSourceReaderReportsFirstInvalidUTF8(t *testing.T) {
	input := string([]byte{'a', '\n', 0xff, 'b', 0xfe})
	_, err := NewSourceReader("invalid.grab", input)
	if err == nil {
		t.Fatal("NewSourceReader() error = nil, want invalid UTF-8 error")
	}

	var encodingErr *InvalidUTF8Error
	if !errors.As(err, &encodingErr) {
		t.Fatalf("NewSourceReader() error type = %T, want *InvalidUTF8Error", err)
	}
	want := Span{Start: Position{Offset: 2, Line: 2, Column: 1}, End: Position{Offset: 3, Line: 2, Column: 2}}
	if encodingErr.Span != want {
		t.Fatalf("InvalidUTF8Error.Span = %+v, want %+v", encodingErr.Span, want)
	}
}

func TestSourceReaderSliceValidatesOffsets(t *testing.T) {
	reader, err := NewSourceReader("test.grab", "aé")
	if err != nil {
		t.Fatalf("NewSourceReader() error = %v", err)
	}

	tests := []struct {
		name string
		span Span
		want string
		ok   bool
	}{
		{name: "valid rune range", span: Span{Start: Position{Offset: 1}, End: Position{Offset: 3}}, want: "é", ok: true},
		{name: "mid-rune start", span: Span{Start: Position{Offset: 2}, End: Position{Offset: 3}}, ok: false},
		{name: "reversed range", span: Span{Start: Position{Offset: 3}, End: Position{Offset: 1}}, ok: false},
		{name: "out of bounds", span: Span{Start: Position{Offset: 0}, End: Position{Offset: 4}}, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := reader.Slice(tt.span)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("Slice(%+v) = (%q, %t), want (%q, %t)", tt.span, got, ok, tt.want, tt.ok)
			}
		})
	}
}
