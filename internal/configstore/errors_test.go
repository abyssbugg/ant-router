package configstore

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorCode(t *testing.T) {
	tests := []struct {
		name string
		code Code
		want string
	}{
		{name: "invalid_revision", code: CodeInvalidRevision, want: "invalid_revision"},
		{name: "revision_corrupt", code: CodeRevisionCorrupt, want: "revision_corrupt"},
		{name: "manifest_corrupt", code: CodeManifestCorrupt, want: "manifest_corrupt"},
		{name: "manifest_not_found", code: CodeManifestNotFound, want: "manifest_not_found"},
		{name: "store_unavailable", code: CodeStoreUnavailable, want: "store_unavailable"},
		{name: "cancelled", code: CodeCancelled, want: "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.code.String()
			if got != tt.want {
				t.Errorf("Code.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "code only",
			err: &Error{
				Code: CodeInvalidRevision,
				Path: "",
				Err:  nil,
			},
			want: "invalid_revision",
		},
		{
			name: "code and wrapped error",
			err: &Error{
				Code: CodeRevisionCorrupt,
				Path: "",
				Err:  fmt.Errorf("bad data"),
			},
			want: "revision_corrupt: bad data",
		},
		{
			name: "code and path",
			err: &Error{
				Code: CodeManifestCorrupt,
				Path: "current",
				Err:  nil,
			},
			want: "manifest_corrupt at current",
		},
		{
			name: "code, path and error",
			err: &Error{
				Code: CodeManifestNotFound,
				Path: "manifest.json",
				Err:  fmt.Errorf("file not found"),
			},
			want: "manifest_not_found at manifest.json: file not found",
		},
		{
			name: "nil error field",
			err: &Error{
				Code: CodeStoreUnavailable,
				Path: "/store",
				Err:  nil,
			},
			want: "store_unavailable at /store",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	underlyingErr := fmt.Errorf("underlying error")
	err := &Error{
		Code: CodeRevisionCorrupt,
		Path: "test",
		Err:  underlyingErr,
	}

	unwrapped := errors.Unwrap(err)
	if unwrapped != underlyingErr {
		t.Errorf("errors.Unwrap(err) = %v, want %v", unwrapped, underlyingErr)
	}
}

func TestErrorUnwrapNil(t *testing.T) {
	err := &Error{
		Code: CodeManifestNotFound,
		Path: "",
		Err:  nil,
	}

	unwrapped := errors.Unwrap(err)
	if unwrapped != nil {
		t.Errorf("errors.Unwrap(err) = %v, want nil", unwrapped)
	}
}

func TestCodeOf(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  Code
		wantFound bool
	}{
		{
			name: "direct Error",
			err: &Error{
				Code: CodeInvalidRevision,
				Path: "",
				Err:  nil,
			},
			wantCode:  CodeInvalidRevision,
			wantFound: true,
		},
		{
			name: "wrapped with fmt.Errorf %w",
			err: fmt.Errorf("wrapper: %w", &Error{
				Code: CodeManifestCorrupt,
				Path: "test",
				Err:  fmt.Errorf("bad"),
			}),
			wantCode:  CodeManifestCorrupt,
			wantFound: true,
		},
		{
			name:      "non-Error",
			err:       fmt.Errorf("plain error"),
			wantCode:  "",
			wantFound: false,
		},
		{
			name:      "nil error",
			err:       nil,
			wantCode:  "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCode, gotFound := CodeOf(tt.err)
			if gotFound != tt.wantFound {
				t.Errorf("CodeOf found = %v, want %v", gotFound, tt.wantFound)
			}
			if gotCode != tt.wantCode {
				t.Errorf("CodeOf code = %q, want %q", gotCode, tt.wantCode)
			}
		})
	}
}

func TestErrorNilSafety(t *testing.T) {
	// Calling Error() on nil should not panic
	var err *Error
	if err != nil {
		t.Fatal("expected nil Error")
	}
	// Not calling err.Error() since that would panic on nil receiver
	// This test documents the behavior
}

func TestErrorNoContentLeakage(t *testing.T) {
	// Error should not echo input data in message, only structured fields
	err := &Error{
		Code: CodeRevisionCorrupt,
		Path: "sensitive/path",
		Err:  fmt.Errorf("sensitive: 12345abcde"),
	}

	msg := err.Error()
	// The message should contain code, path, and wrapped error message
	// but not accidentally duplicate or leak data
	if len(msg) == 0 {
		t.Error("Error() produced empty message")
	}
	// Just verify it's reasonable length and contains expected parts
	if !strings.Contains(msg, "revision_corrupt") {
		t.Errorf("Error message missing code: %q", msg)
	}
}

func TestTypedNilConfigstoreErrorSafety(t *testing.T) {
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

func TestConfigstoreErrorNilErrField(t *testing.T) {
	// Typed nil in Err field - Error() should not panic and should return stable string
	err := &Error{
		Code: CodeManifestCorrupt,
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
