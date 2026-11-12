// Package data implements the host-owned database boundary for GRAB.
package data

// Error contains only safe, operator-independent diagnostics. Driver errors,
// SQL, parameters and connection details must never be attached to it.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(code, message string) error { return &Error{Code: code, Message: message} }

const (
	Invalid       = "GRAB-E210"
	Operation     = "GRAB-E313"
	Type          = "GRAB-E314"
	Ownership     = "GRAB-E315"
	Migration     = "GRAB-E316"
	Limit         = "GRAB-E317"
	UnknownCommit = "GRAB-E318"
	Denied        = "GRAB-E403"
)
