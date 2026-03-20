// Package exceptions provides domain-level error types for QueueBuzz.
//
// All custom errors implement the standard error interface and are designed
// to be matched with errors.Is / errors.As in the global error handler.
// Services return these errors; handlers simply bubble them up.
package exceptions

import "fmt"

// ---------------------------------------------------------------------------
// Sentinel errors – use errors.Is(err, exceptions.ErrNotFound) etc.
// ---------------------------------------------------------------------------

var (
	ErrNotFound     = NewAppException(404, "Resource not found")
	ErrUnauthorized = NewAppException(401, "Unauthorized")
	ErrForbidden    = NewAppException(403, "Forbidden")
	ErrConflict     = NewAppException(409, "Conflict")
	ErrGone         = NewAppException(410, "Resource is no longer available")
	ErrTooMany      = NewAppException(429, "Too many requests")
	ErrInternal     = NewAppException(500, "Internal server error")
)

// ---------------------------------------------------------------------------
// AppException – generic HTTP-aware application error.
// ---------------------------------------------------------------------------

// AppException carries an HTTP status code and a human-readable message.
// It is the base for all custom errors in the application.
type AppException struct {
	Code    int    `json:"-"`
	Message string `json:"message"`
}

func (e *AppException) Error() string { return e.Message }

// NewAppException creates a generic application error.
func NewAppException(code int, message string) *AppException {
	return &AppException{Code: code, Message: message}
}

// ---------------------------------------------------------------------------
// FieldException – single-field validation / business-rule error.
// ---------------------------------------------------------------------------

// FieldException represents a validation or business-rule error that is
// scoped to a specific request field (e.g. "email", "phone").
// The global error handler formats it as:
//
//	{ "message": "Validation Failed", "error": { "<Field>": "<Message>" } }
type FieldException struct {
	Code    int
	Field   string
	Message string
}

func (e *FieldException) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// NewFieldException creates a field-level error with the given HTTP code.
func NewFieldException(code int, field, message string) *FieldException {
	return &FieldException{Code: code, Field: field, Message: message}
}

// ---------------------------------------------------------------------------
// Convenience constructors for common scenarios.
// ---------------------------------------------------------------------------

// Duplicate returns a 429 FieldException indicating a duplicate value.
func Duplicate(field, message string) *FieldException {
	return NewFieldException(429, field, message)
}

// BadField returns a 400 FieldException for a malformed or invalid field.
func BadField(field, message string) *FieldException {
	return NewFieldException(400, field, message)
}
