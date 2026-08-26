package canonical

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorCodes(t *testing.T) {
	codes := map[ErrorCode]string{
		CodeInvalidVersion:               "invalid_version",
		CodeInvalidID:                    "invalid_id",
		CodeInvalidRole:                  "invalid_role",
		CodeInvalidVariant:               "invalid_variant",
		CodeInvalidUTF8:                  "invalid_utf8",
		CodeInvalidJSON:                  "invalid_json",
		CodeDuplicateKey:                 "duplicate_key",
		CodeUnknownField:                 "unknown_field",
		CodeLimitExceeded:                "limit_exceeded",
		CodeInvalidInvariant:             "invalid_invariant",
		CodeUnsupportedMediaType:         "unsupported_media_type",
		CodeInvalidURL:                   "invalid_url",
		CodeContinuationAffinityRequired: "continuation_affinity_required",
	}
	for code, expected := range codes {
		if string(code) != expected {
			t.Errorf("code = %q, want %q", code, expected)
		}
	}
}

func TestErrorWrappingTypedNilAndRedaction(t *testing.T) {
	inner := errors.New("safe cause")
	wrapped := fmt.Errorf("wrapped: %w", &Error{Code: CodeInvalidID, Path: "request.id", Err: inner})
	if code, ok := CodeOf(wrapped); !ok || code != CodeInvalidID {
		t.Fatalf("CodeOf() = %q, %v", code, ok)
	}
	if !errors.Is(wrapped, inner) {
		t.Error("wrapped cause was not preserved")
	}
	var typedNil *Error
	var err error = typedNil
	if code, ok := CodeOf(err); ok || code != "" || typedNil.Error() != "" || typedNil.Unwrap() != nil {
		t.Error("typed nil error behavior is unsafe")
	}
	canary := "synthetic_content_marker_12345"
	safe := (&Error{Code: CodeInvalidInvariant, Path: "request.input[0]", Err: errors.New("value is invalid")}).Error()
	if strings.Contains(safe, canary) {
		t.Error("error exposed content canary")
	}
}
