package controltransport

import (
	"errors"
	"fmt"
)

// Code is a stable control-transport error category.
type Code string

const (
	CodeInvalidInstance      Code = "invalid_instance"
	CodeInvalidEndpoint      Code = "invalid_endpoint"
	CodePathResolutionFailed Code = "path_resolution_failed"
	CodeAlreadyRunning       Code = "already_running"
	CodePermissionDenied     Code = "permission_denied"
	CodeCancelled            Code = "cancelled"
)

// Error carries a stable code, safe field path, and wrapped cause.
type Error struct {
	Code Code
	Path string
	Err  error
}

// Error returns a safe, stable representation of the transport error.
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

// CodeOf extracts a control-transport code through wrapped errors.
func CodeOf(err error) (Code, bool) {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code, true
	}
	return "", false
}
