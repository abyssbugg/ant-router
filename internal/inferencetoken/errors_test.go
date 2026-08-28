package inferencetoken

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorCodes(t *testing.T) {
	tests := map[Code]string{
		CodeInvalidToken:        "invalid_token",
		CodeGenerationFailed:    "generation_failed",
		CodeTokenNotFound:       "token_not_found",
		CodeTokenCorrupt:        "token_corrupt",
		CodeStoreUnavailable:    "store_unavailable",
		CodePermissionDenied:    "permission_denied",
		CodeCancelled:           "cancelled",
		CodeDurabilityUncertain: "durability_uncertain",
	}
	for code, expected := range tests {
		if string(code) != expected {
			t.Errorf("code = %q, want %q", code, expected)
		}
	}
}

func TestErrorWrappingAndTypedNil(t *testing.T) {
	inner := errors.New("cause")
	wrapped := fmt.Errorf("wrapped: %w", &Error{Code: CodeTokenCorrupt, Path: "token_file", Err: inner})
	if code, ok := CodeOf(wrapped); !ok || code != CodeTokenCorrupt {
		t.Fatalf("CodeOf() = %q, %v", code, ok)
	}
	if !errors.Is(wrapped, inner) {
		t.Error("wrapped cause not preserved")
	}
	var typedNil *Error
	var err error = typedNil
	if code, ok := CodeOf(err); ok || code != "" {
		t.Fatalf("CodeOf(typed nil) = %q, %v", code, ok)
	}
	if typedNil.Error() != "" || typedNil.Unwrap() != nil {
		t.Error("typed nil methods are not safe")
	}
}
