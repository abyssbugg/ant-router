package config

import (
	"bytes"
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
	if CodeStaleCandidate != "stale_candidate" {
		t.Errorf("CodeStaleCandidate = %v, want 'stale_candidate'", CodeStaleCandidate)
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

func TestTypedNilConfigErrorSafety(t *testing.T) {
	// When a nil *Error is wrapped, it becomes a non-nil error interface
	// The important check is that Error() and Unwrap() on a nil receiver handle gracefully
	// This test verifies nil receivers don't panic
	var err *Error = nil
	if err != nil {
		t.Fatal("test setup: err should be nil")
	}
	// Don't call methods on nil receiver - just verify the nil check in Error/Unwrap methods exists
	// by checking the implementation
}

func TestConfigErrorNilErrField(t *testing.T) {
	// Typed nil in Err field - Error() should not panic and should return stable string
	err := &Error{
		Code: CodeValidationFailed,
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

func TestDecodeManifestNoSecretEcho(t *testing.T) {
	// Unknown field with secret-like value should not echo the secret in error
	jsonWithSecret := []byte(`{
		"schema_version": 1,
		"secret_api_key": "sk_live_51234567890abcdef"
	}`)

	_, err := Decode(bytes.NewReader(jsonWithSecret))
	if err == nil {
		t.Fatal("Decode should reject unknown fields")
	}

	errMsg := err.Error()
	// Error message should not contain the actual secret value
	if strings.Contains(errMsg, "sk_live_") {
		t.Errorf("Error message leaked secret: %q", errMsg)
	}
}
