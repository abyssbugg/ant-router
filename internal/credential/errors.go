package credential

import (
	"errors"
	"fmt"
)

// Code is a stable credential error category.
type Code string

const (
	CodeInvalidReference Code = "invalid_reference"
	CodeInvalidSource    Code = "invalid_source"
	CodeInvalidContext   Code = "invalid_context"
	CodeMissing          Code = "missing"
	CodeCancelled        Code = "cancelled"
)

// Error carries a stable code, safe path, and safe underlying cause.
type Error struct {
	Code Code
	Path string
	Err  error
}

func newError(code Code, path string, cause error) *Error {
	return &Error{Code: code, Path: path, Err: cause}
}

// Error returns a stable message without credential values or cause text.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Path != "" {
		return fmt.Sprintf("%s at %s", e.Code, e.Path)
	}
	return string(e.Code)
}

// Unwrap returns the safe underlying cause.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CodeOf extracts a credential error code through wrapping.
func CodeOf(err error) Code {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code
	}
	return ""
}
