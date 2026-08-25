package canonical

import (
	"errors"
	"fmt"
)

// ErrorCode is a stable canonical validation error category.
type ErrorCode string

const (
	CodeInvalidVersion               ErrorCode = "invalid_version"
	CodeInvalidID                    ErrorCode = "invalid_id"
	CodeInvalidRole                  ErrorCode = "invalid_role"
	CodeInvalidVariant               ErrorCode = "invalid_variant"
	CodeInvalidUTF8                  ErrorCode = "invalid_utf8"
	CodeInvalidJSON                  ErrorCode = "invalid_json"
	CodeDuplicateKey                 ErrorCode = "duplicate_key"
	CodeUnknownField                 ErrorCode = "unknown_field"
	CodeLimitExceeded                ErrorCode = "limit_exceeded"
	CodeInvalidInvariant             ErrorCode = "invalid_invariant"
	CodeUnsupportedMediaType         ErrorCode = "unsupported_media_type"
	CodeInvalidURL                   ErrorCode = "invalid_url"
	CodeContinuationAffinityRequired ErrorCode = "continuation_affinity_required"
)

// Error carries a stable code, safe field path, and wrapped safe cause.
type Error struct {
	Code ErrorCode
	Path string
	Err  error
}

// Error returns a safe canonical validation message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Path != "" && e.Err != nil:
		return fmt.Sprintf("%s at %s: %v", e.Code, e.Path, e.Err)
	case e.Path != "":
		return fmt.Sprintf("%s at %s", e.Code, e.Path)
	case e.Err != nil:
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	default:
		return string(e.Code)
	}
}

// Unwrap returns the underlying safe cause.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CodeOf extracts a canonical error code through wrapped errors.
func CodeOf(err error) (ErrorCode, bool) {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code, true
	}
	return "", false
}
