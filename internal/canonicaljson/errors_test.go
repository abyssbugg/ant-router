package canonicaljson

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorCodesAndWrapping(t *testing.T) {
	codes := map[ErrorCode]string{
		CodeInvalidUTF8:   "invalid_utf8",
		CodeInvalidJSON:   "invalid_json",
		CodeDuplicateKey:  "duplicate_key",
		CodeLimitExceeded: "limit_exceeded",
	}
	for code, want := range codes {
		if string(code) != want {
			t.Errorf("code = %q, want %q", code, want)
		}
	}
	inner := errors.New("safe cause")
	wrapped := fmt.Errorf("wrapped: %w", &Error{Code: CodeInvalidJSON, Path: "$", Err: inner})
	if code, ok := CodeOf(wrapped); !ok || code != CodeInvalidJSON || !errors.Is(wrapped, inner) {
		t.Errorf("wrapped error = %v", wrapped)
	}
	if code, ok := CodeOf(errors.New("not canonical JSON")); ok || code != "" {
		t.Errorf("CodeOf(noncanonical) = %q, %v", code, ok)
	}
	var typedNil *Error
	var err error = typedNil
	if code, ok := CodeOf(err); ok || code != "" || typedNil.Error() != "" || typedNil.Unwrap() != nil {
		t.Error("typed nil error behavior is unsafe")
	}
}
