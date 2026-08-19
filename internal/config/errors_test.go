package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCodeConstantsLowercase(t *testing.T) {
	// Error codes must be lowercase strings
	if CodeValidationFailed != "validation_failed" {
		t.Errorf("CodeValidationFailed = %v, want 'validation_failed'", CodeValidationFailed)
	}
	if CodeUnsupportedSchema != "unsupported_schema" {
		t.Errorf("CodeUnsupportedSchema = %v, want 'unsupported_schema'", CodeUnsupportedSchema)
	}
	if CodeCancelled != "cancelled" {
		t.Errorf("CodeCancelled = %v, want 'cancelled'", CodeCancelled)
	}
	if CodePathResolutionFailed != "path_resolution_failed" {
		t.Errorf("CodePathResolutionFailed = %v, want 'path_resolution_failed'", CodePathResolutionFailed)
	}
}

func TestErrorType(t *testing.T) {
	err := &Error{
		Code: CodeValidationFailed,
		Path: "daemon.shutdown_timeout",
		Err:  errors.New("must be at least 1s"),
	}

	if err.Code != CodeValidationFailed {
		t.Errorf("Code mismatch: got %v, want %v", err.Code, CodeValidationFailed)
	}
	if err.Path != "daemon.shutdown_timeout" {
		t.Errorf("Path mismatch: got %v, want %v", err.Path, "daemon.shutdown_timeout")
	}
	if err.Err == nil {
		t.Error("Err is nil")
	}
}

func TestErrorMethodString(t *testing.T) {
	err := &Error{
		Code: CodeValidationFailed,
		Path: "daemon.shutdown_timeout",
		Err:  errors.New("must be at least 1s"),
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string")
	}
	// Should contain code and path
	if !strings.Contains(errStr, "validation_failed") {
		t.Errorf("Error() should contain lowercase code: %v", errStr)
	}
	if !strings.Contains(errStr, "daemon.shutdown_timeout") {
		t.Errorf("Error() should contain path: %v", errStr)
	}
}

func TestErrorMethodNilErr(t *testing.T) {
	err := &Error{
		Code: CodeValidationFailed,
		Path: "daemon.shutdown_timeout",
		Err:  nil,
	}

	// Should not panic and should return stable string
	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string for nil Err")
	}
	// Should still contain code and path info
	if !strings.Contains(errStr, "validation_failed") {
		t.Errorf("Error() should contain code even when Err is nil: %v", errStr)
	}
}

func TestErrorUnwrap(t *testing.T) {
	inner := errors.New("must be at least 1s")
	err := &Error{
		Code: CodeValidationFailed,
		Path: "daemon.shutdown_timeout",
		Err:  inner,
	}

	unwrapped := err.Unwrap()
	if !errors.Is(unwrapped, inner) {
		t.Errorf("Unwrap() failed: got %v, want %v", unwrapped, inner)
	}
}

func TestCodeOfFunctionWrappedWithFmt(t *testing.T) {
	inner := &Error{
		Code: CodeValidationFailed,
		Path: "daemon.shutdown_timeout",
		Err:  errors.New("must be at least 1s"),
	}
	wrapped := fmt.Errorf("wrapped: %w", inner)

	code, ok := CodeOf(wrapped)
	if !ok {
		t.Error("CodeOf() returned false for wrapped Error type")
	}
	if code != CodeValidationFailed {
		t.Errorf("CodeOf() code mismatch: got %v, want %v", code, CodeValidationFailed)
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
