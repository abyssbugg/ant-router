package canonical

import (
	"strings"
	"testing"
	"time"
)

func TestFailure(t *testing.T) {
	retryAfter := 5 * time.Second
	failure, err := NewFailure(FailureParams{Code: FailureCode("rate_limited"), Message: "rate limited", Retryable: true, RetryAfter: &retryAfter})
	if err != nil || !failure.Valid() || failure.Code() != FailureCode("rate_limited") || failure.Message() != "rate limited" || !failure.Retryable() {
		t.Fatalf("NewFailure() = %#v, %v", failure, err)
	}
	if value, ok := failure.RetryAfter(); !ok || value != retryAfter {
		t.Errorf("RetryAfter() = %v, %v", value, ok)
	}
}

func TestFailureRejectsInvalid(t *testing.T) {
	negative := -time.Second
	tooLong := MaxRetryAfter + time.Second
	tests := []FailureParams{
		{Code: FailureCode("unknown"), Message: "error"},
		{Code: FailureCode("internal_error"), Message: ""},
		{Code: FailureCode("internal_error"), Message: string([]byte{0xff})},
		{Code: FailureCode("internal_error"), Message: strings.Repeat("x", MaxFailureMessageBytes+1)},
		{Code: FailureCode("authentication_failed"), Message: "auth", Retryable: true},
		{Code: FailureCode("rate_limited"), Message: "rate", RetryAfter: pointer(negative)},
		{Code: FailureCode("rate_limited"), Message: "rate", Retryable: true, RetryAfter: pointer(tooLong)},
		{Code: FailureCode("cancelled"), Message: "cancelled", Retryable: true},
	}
	for _, params := range tests {
		if _, err := NewFailure(params); err == nil {
			t.Errorf("invalid failure accepted: %#v", params)
		}
	}
}

func TestFailureConstructorErrorDoesNotEchoMessage(t *testing.T) {
	canary := strings.Repeat("synthetic_failure_marker", MaxFailureMessageBytes)
	_, err := NewFailure(FailureParams{Code: FailureCode("internal_error"), Message: canary})
	if err == nil || strings.Contains(err.Error(), canary) {
		t.Errorf("failure validation leaked message: %v", err)
	}
}
