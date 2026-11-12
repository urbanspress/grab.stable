package interpreter

import (
	"errors"
	"fmt"
	"strings"

	"grab/internal/lexer"
	"grab/internal/parser"
)

// DiagnosticMode selects how diagnostics are formatted for the target audience.
type DiagnosticMode string

const (
	ModeDeveloper  DiagnosticMode = "developer"
	ModeBeginner   DiagnosticMode = "beginner"
	ModeProduction DiagnosticMode = "production"
)

// ParseDiagnosticMode parses a diagnostic mode flag value ("beginner", "developer", or "production").
func ParseDiagnosticMode(raw string) (DiagnosticMode, bool) {
	switch DiagnosticMode(strings.TrimSpace(raw)) {
	case ModeDeveloper:
		return ModeDeveloper, true
	case ModeBeginner:
		return ModeBeginner, true
	case ModeProduction:
		return ModeProduction, true
	default:
		return "", false
	}
}

// ExtractErrorMeta extracts the stable GRAB 0.4 error code, category, source span,
// and optional call stack from any lexer, parser, or interpreter error.
func ExtractErrorMeta(err error) (code string, category string, span lexer.Span, stack []StackFrame) {
	var utf8Err *lexer.InvalidUTF8Error
	if errors.As(err, &utf8Err) {
		return utf8Err.Code(), "invalid source encoding", utf8Err.Span, nil
	}
	var lexErr *lexer.LexError
	if errors.As(err, &lexErr) {
		return lexErr.Code(), lexErr.Kind.String(), lexErr.Span, nil
	}
	var parseErr *parser.ParseError
	if errors.As(err, &parseErr) {
		switch parseErr.Kind {
		case parser.ParseDuplicateMapKey:
			return parseErr.Code(), "duplicate map key", parseErr.Span, nil
		case parser.ParseDuplicateRecordField:
			return parseErr.Code(), "duplicate record field", parseErr.Span, nil
		default:
			return parseErr.Code(), "unexpected token or malformed statement", parseErr.Span, nil
		}
	}
	var interpErr *Error
	if errors.As(err, &interpErr) {
		return interpErr.Code(), interpErr.Kind.String(), interpErr.Span, interpErr.Stack
	}
	return "GRAB-E301", "runtime error", lexer.Span{}, nil
}

// FormatDiagnostic formats an error according to the selected GRAB 0.4 diagnostic mode.
func FormatDiagnostic(source string, err error, mode DiagnosticMode) string {
	if mode == "" {
		mode = ModeDeveloper
	}
	code, category, span, stack := ExtractErrorMeta(err)

	if mode == ModeProduction {
		return fmt.Sprintf("grab: [%s] %s", code, category)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "grab: [%s] %v", code, err)
	for _, frame := range stack {
		fmt.Fprintf(&b, "\n  in %s called at line %d, column %d", frame.Function, frame.Span.Start.Line, frame.Span.Start.Column)
	}

	if mode == ModeBeginner {
		if snippet := extractSourceLine(source, span.Start.Line); snippet != "" {
			fmt.Fprintf(&b, "\n  %d | %s", span.Start.Line, snippet)
		}
		fmt.Fprintf(&b, "\n  Hint: %s", beginnerHint(code, err))
	}
	return b.String()
}

func extractSourceLine(source string, lineNum int) string {
	if source == "" || lineNum < 1 {
		return ""
	}
	normalized := strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if lineNum > len(lines) {
		return ""
	}
	return strings.TrimRight(lines[lineNum-1], " \t")
}

func beginnerHint(code string, err error) string {
	switch code {
	case "GRAB-E001":
		return "Save your .grab file with standard UTF-8 text encoding."
	case "GRAB-E002":
		return "Remove or replace unsupported punctuation; GRAB uses English words like 'plus', 'minus', 'is', and 'joined with'."
	case "GRAB-E003":
		return "Inside text quotes, valid escapes are \\\\, \\\", \\n, \\r, and \\t."
	case "GRAB-E004":
		return "Close the text string with a matching double quote (\") on the same line."
	case "GRAB-E005", "GRAB-E007":
		return "Byte literals start with b\" and end with \" on the same line using ASCII characters or \\xNN hex escapes."
	case "GRAB-E006":
		return "Write numbers like 42, 19.95 (with digits on both sides of the dot), or 1.25e-2."
	case "GRAB-E101":
		return "Check the statement keywords and make sure blocks like 'if', 'repeat', 'for each', 'while', 'make', and 'try' close with 'end'."
	case "GRAB-E102":
		return "Each key in a map must be unique. Remove or rename the duplicate key."
	case "GRAB-E103":
		return "Each field name in a record must be unique. Remove or rename the duplicate field."
	case "GRAB-E201":
		var interpErr *Error
		if errors.As(err, &interpErr) && interpErr.Name != "" {
			return fmt.Sprintf("Define %q first using 'set %s to ...' before reading it, or check its spelling.", interpErr.Name, interpErr.Name)
		}
		return "Define the variable using 'set <name> to ...' before reading it."
	case "GRAB-E202":
		return "Use a distinct name; module imports, functions, parameters, and top-level variables cannot share the same name."
	case "GRAB-E203":
		return "Check the function or module name. For standard library calls like 'text.from', add 'use text' at the top of the file."
	case "GRAB-E204":
		return "Pass the exact number of arguments expected by the function after 'using'."
	case "GRAB-E205":
		return "'give back' can only be used inside a 'make ... end' function."
	case "GRAB-E208":
		return "Each task in a 'do together ... end' block must set a distinct variable name (up to 16 tasks per block)."
	case "GRAB-E301":
		return "Check that the values have compatible types (for example, use 'text.from using <number>' before 'joined with')."
	case "GRAB-E302":
		return "Check that the divisor is not 0 before dividing or taking modulo, or wrap the calculation in 'try ... catch err ... end'."
	case "GRAB-E303":
		return "Check whether the field or key exists first using 'has' (for example, 'if product has price')."
	case "GRAB-E304":
		return "List indexes start at 0 and must be less than the list length."
	case "GRAB-E305":
		return "The count in 'repeat <count> times' must be 0 or a positive integer."
	case "GRAB-E306":
		return "A function called itself too many times (over 256 frames). Make sure recursive functions have a stopping condition."
	case "GRAB-E307":
		return "A loop ran for more than 1,000,000 steps. Make sure your loop condition eventually stops."
	case "GRAB-E308":
		return "This error was raised by 'raise'. Wrap the call in 'try ... catch err ... end' to handle it gracefully."
	case "GRAB-E309":
		return "Check the input passed to the standard library function, or catch invalid input with 'try ... catch err ... end'."
	case "GRAB-E310":
		return "Check the URL and server status, or wrap the HTTP call in 'try ... catch err ... end' to handle network failures."
	case "GRAB-E311":
		return "The server took too long to respond. Increase 'timeout_ms' in options or wrap the call in 'try ... catch err ... end'."
	case "GRAB-E401":
		return "File access is locked by default. Grant folder access with '--allow-read <dir>' or '--allow-write <dir>' (sensitive files like .env and .git are always blocked)."
	case "GRAB-E402":
		return "Network access is locked by default. Re-run with '--allow-net <host>' to allow requests to this host (private/metadata IPs are blocked by default)."
	default:
		return "Review the line and column reported above to fix the issue."
	}
}
