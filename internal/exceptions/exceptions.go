package exceptions

import "fmt"

var (
	ErrNotFound     = NewAppException(404, "Resource not found")
	ErrUnauthorized = NewAppException(401, "Unauthorized")
	ErrForbidden    = NewAppException(403, "Forbidden")
	ErrConflict     = NewAppException(409, "Conflict")
	ErrGone         = NewAppException(410, "Resource is no longer available")
	ErrTooMany      = NewAppException(429, "Too many requests")
	ErrInternal     = NewAppException(500, "Internal server error")
)

type AppException struct {
	Code    int    `json:"-"`
	Message string `json:"message"`
}

func (e *AppException) Error() string { return e.Message }

func NewAppException(code int, message string) *AppException {
	return &AppException{Code: code, Message: message}
}

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

func Duplicate(field, message string) *FieldException {
	return NewFieldException(429, field, message)
}
