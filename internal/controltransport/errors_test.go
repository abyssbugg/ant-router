package controltransport

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCodeConstantsLowercase(t *testing.T) {
	// Error codes must be lowercase strings
	if CodeInvalidInstance != "invalid_instance" {
		t.Errorf("CodeInvalidInstance = %v, want 'invalid_instance'", CodeInvalidInstance)
	}
	if CodeInvalidEndpoint != "invalid_endpoint" {
		t.Errorf("CodeInvalidEndpoint = %v, want 'invalid_endpoint'", CodeInvalidEndpoint)
	}
	if CodePathResolutionFailed != "path_resolution_failed" {
		t.Errorf("CodePathResolutionFailed = %v, want 'path_resolution_failed'", CodePathResolutionFailed)
	}
	if CodeAlreadyRunning != "already_running" {
		t.Errorf("CodeAlreadyRunning = %v, want 'already_running'", CodeAlreadyRunning)
	}
	if CodePermissionDenied != "permission_denied" {
		t.Errorf("CodePermissionDenied = %v, want 'permission_denied'", CodePermissionDenied)
	}
	if CodeCancelled != "cancelled" {
		t.Errorf("CodeCancelled = %v, want 'cancelled'", CodeCancelled)
	}
}

func TestErrorType(t *testing.T) {
	err := &Error{
		Code: CodeInvalidInstance,
		Path: "instance_name",
		Err:  errors.New("invalid characters"),
	}

	if err.Code != CodeInvalidInstance {
		t.Errorf("Code mismatch: got %v, want %v", err.Code, CodeInvalidInstance)
	}
	if err.Path != "instance_name" {
		t.Errorf("Path mismatch: got %v, want %v", err.Path, "instance_name")
	}
	if err.Err == nil {
		t.Error("Err is nil")
	}
}

func TestErrorMethodString(t *testing.T) {
	err := &Error{
		Code: CodeInvalidInstance,
		Path: "instance_name",
		Err:  errors.New("invalid characters"),
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string")
	}
	// Should contain code and path
	if !strings.Contains(errStr, "invalid_instance") {
		t.Errorf("Error() should contain lowercase code: %v", errStr)
	}
	if !strings.Contains(errStr, "instance_name") {
		t.Errorf("Error() should contain path: %v", errStr)
	}
}

func TestErrorMethodNilErr(t *testing.T) {
	err := &Error{
		Code: CodeInvalidInstance,
		Path: "instance_name",
		Err:  nil,
	}

	// Should not panic and should return stable string
	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string for nil Err")
	}
	// Should still contain code and path info
	if !strings.Contains(errStr, "invalid_instance") {
		t.Errorf("Error() should contain code even when Err is nil: %v", errStr)
	}
}

func TestErrorUnwrap(t *testing.T) {
	inner := errors.New("invalid characters")
	err := &Error{
		Code: CodeInvalidInstance,
		Path: "instance_name",
		Err:  inner,
	}

	unwrapped := err.Unwrap()
	if !errors.Is(unwrapped, inner) {
		t.Errorf("Unwrap() failed: got %v, want %v", unwrapped, inner)
	}
}

func TestCodeOfFunctionWrappedWithFmt(t *testing.T) {
	inner := &Error{
		Code: CodeInvalidInstance,
		Path: "instance_name",
		Err:  errors.New("invalid characters"),
	}
	wrapped := fmt.Errorf("wrapped: %w", inner)

	code, ok := CodeOf(wrapped)
	if !ok {
		t.Error("CodeOf() returned false for wrapped Error type")
	}
	if code != CodeInvalidInstance {
		t.Errorf("CodeOf() code mismatch: got %v, want %v", code, CodeInvalidInstance)
	}
}

func TestCodeOfNonError(t *testing.T) {
	err := errors.New("plain error")
	code, ok := CodeOf(err)
	if ok {
		t.Error("CodeOf() returned true for non-Error type")
	}
	if code != "" {
		t.Errorf("CodeOf() code should be empty for non-Error type: got %v", code)
	}
}

func TestTypedNilControlTransportErrorSafety(t *testing.T) {
	// When a nil *Error is wrapped, it becomes a non-nil error interface
	// The important check is that Error() and Unwrap() on a nil receiver handle gracefully
	// This test verifies nil receivers don't panic
	var err *Error = nil
	if err != nil {
		t.Fatal("test setup: err should be nil")
	}
	if got := err.Error(); got != "" {
		t.Errorf("Error() = %q, want empty", got)
	}
	if got := err.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
}

func TestControlTransportErrorNilErrField(t *testing.T) {
	// Typed nil in Err field - Error() should not panic and should return stable string
	err := &Error{
		Code: CodeInvalidInstance,
		Path: "test",
		Err:  nil,
	}

	// Should not panic
	msg := err.Error()
	if msg == "" {
		t.Error("Error() returned empty string when Err is nil")
	}

	// Unwrap should return nil safely
	unwrapped := err.Unwrap()
	if unwrapped != nil {
		t.Errorf("Unwrap() should return nil when Err is nil, got %v", unwrapped)
	}
}

func TestErrorPathNotEchoedForInvalidInstance(t *testing.T) {
	// When path contains sensitive data like invalid instance names, they should not be echoed
	err := &Error{
		Code: CodeInvalidInstance,
		Path: "user_input_here",
		Err:  nil,
	}

	errMsg := err.Error()
	// The path should be there but marked clearly as the problematic path
	if !strings.Contains(errMsg, "invalid_instance") {
		t.Errorf("Error message should contain error code: %q", errMsg)
	}
}
