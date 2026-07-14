package session

import "fmt"

type ErrorKind string

const (
	ErrorInvalidName      ErrorKind = "invalid_name"
	ErrorConflict         ErrorKind = "unmanaged_name_conflict"
	ErrorNotFound         ErrorKind = "managed_session_not_found"
	ErrorDependency       ErrorKind = "dependency_failure"
	ErrorBridgeIncomplete ErrorKind = "bridge_startup_incomplete"
)

type Error struct {
	Kind      ErrorKind
	Operation string
	Session   string
	Err       error
}

func (e *Error) Error() string {
	message := string(e.Kind)
	if e.Operation != "" {
		message = e.Operation + ": " + message
	}
	if e.Session != "" {
		message += fmt.Sprintf(" for session %q", e.Session)
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *Error) Unwrap() error {
	return e.Err
}

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e.Kind == other.Kind
}

var (
	ErrInvalidName      = &Error{Kind: ErrorInvalidName}
	ErrConflict         = &Error{Kind: ErrorConflict}
	ErrNotFound         = &Error{Kind: ErrorNotFound}
	ErrDependency       = &Error{Kind: ErrorDependency}
	ErrBridgeIncomplete = &Error{Kind: ErrorBridgeIncomplete}
)

func lifecycleError(kind ErrorKind, operation, sessionID string, err error) error {
	return &Error{Kind: kind, Operation: operation, Session: sessionID, Err: err}
}
