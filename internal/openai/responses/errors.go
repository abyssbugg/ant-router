package responses

import (
	"errors"
	"fmt"
)

// ErrorCode is a stable Responses request-adapter error category.
type ErrorCode string

const (
	CodeInvalidJSON      ErrorCode = "invalid_json"
	CodeUnknownField     ErrorCode = "unknown_field"
	CodeUnsupportedField ErrorCode = "unsupported_field"
	CodeInvalidField     ErrorCode = "invalid_field"
	CodeLimitExceeded    ErrorCode = "limit_exceeded"
	CodeInternal         ErrorCode = "internal"
)

// Error carries a stable code, safe request path, and safe wrapped cause.
type Error struct {
	Code ErrorCode
	Path string
	Err  error
}

func newError(code ErrorCode, path string, cause error) *Error {
	return &Error{Code: code, Path: path, Err: cause}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Path != "" {
		return fmt.Sprintf("%s at %s", e.Code, e.Path)
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CodeOf extracts a Responses adapter code through wrapped errors.
func CodeOf(err error) ErrorCode {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code
	}
	return ""
}

// PathOf extracts a safe Responses request path through wrapped errors.
func PathOf(err error) string {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Path
	}
	return ""
}
