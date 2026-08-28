package execution

import (
	"errors"
	"fmt"
	"time"
)

// ErrorCode is a stable provider-execution error category.
type ErrorCode string

const (
	CodeInvalidError ErrorCode = "invalid_error"
	CodeInvalidModel ErrorCode = "invalid_model"
	CodeUnavailable  ErrorCode = "unavailable"
	CodeRateLimited  ErrorCode = "rate_limited"
	CodeInternal     ErrorCode = "internal"
)

// Error carries a stable code, safe path, optional retry delay, and safe cause.
type Error struct {
	Code       ErrorCode
	Path       string
	RetryDelay time.Duration
	Err        error
}

// NewError constructs an execution error and converts invalid metadata into CodeInvalidError.
func NewError(code ErrorCode, path string, retryAfter time.Duration, cause error) *Error {
	if !code.Valid() || retryAfter < 0 || code != CodeRateLimited && retryAfter != 0 {
		return &Error{Code: CodeInvalidError, Path: "error"}
	}
	return &Error{Code: code, Path: path, RetryDelay: retryAfter, Err: cause}
}

// Valid reports whether code is a supported execution category.
func (code ErrorCode) Valid() bool {
	switch code {
	case CodeInvalidError, CodeInvalidModel, CodeUnavailable, CodeRateLimited, CodeInternal:
		return true
	default:
		return false
	}
}

// Error returns a stable message without provider request or credential values.
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

// CodeOf extracts a stable code through wrapped errors.
func CodeOf(err error) ErrorCode {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code
	}
	return ""
}

// RetryAfter extracts a validated retry delay through wrapped errors.
func RetryAfter(err error) time.Duration {
	var target *Error
	if errors.As(err, &target) && target != nil && target.Code == CodeRateLimited && target.RetryDelay > 0 {
		return target.RetryDelay
	}
	return 0
}
