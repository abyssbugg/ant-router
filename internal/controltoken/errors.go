package controltoken

import (
	"errors"
	"fmt"
)

// Code is a stable control-token error category.
type Code string

const (
	CodeInvalidToken        Code = "invalid_token"
	CodeGenerationFailed    Code = "generation_failed"
	CodeTokenNotFound       Code = "token_not_found"
	CodeTokenCorrupt        Code = "token_corrupt"
	CodeStoreUnavailable    Code = "store_unavailable"
	CodePermissionDenied    Code = "permission_denied"
	CodeCancelled           Code = "cancelled"
	CodeDurabilityUncertain Code = "durability_uncertain"
)

// Error carries a stable code, safe path, and wrapped cause.
type Error struct {
	Code Code
	Path string
	Err  error
}

// Error returns a safe representation of the token error.
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

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CodeOf extracts a token error code through wrapped errors.
func CodeOf(err error) (Code, bool) {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code, true
	}
	return "", false
}
