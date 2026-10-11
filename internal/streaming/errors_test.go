package streaming

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorCodesAndWrapping(t *testing.T) {
	codes := map[ErrorCode]string{
		CodeInvalidSequence: "invalid_sequence", CodeResponseMismatch: "response_mismatch", CodeDuplicateEvent: "duplicate_event", CodeInvalidItemState: "invalid_item_state", CodeInvalidContentState: "invalid_content_state", CodeCompletionMismatch: "completion_mismatch", CodeUsageRegression: "usage_regression", CodeTerminalMismatch: "terminal_mismatch", CodeEventAfterTerminal: "event_after_terminal", CodeResponseLimitExceeded: "response_limit_exceeded", CodePrecommitLimitExceeded: "precommit_limit_exceeded", CodeEventLimitExceeded: "event_limit_exceeded", CodeSourceFailed: "source_failed", CodeSinkFailed: "sink_failed", CodeDownstreamStalled: "downstream_stalled", CodeCancelled: "cancelled", CodeCloseFailed: "close_failed",
	}
	for code, want := range codes {
		if string(code) != want {
			t.Errorf("code=%q want=%q", code, want)
		}
	}
	inner := errors.New("safe cause")
	wrapped := fmt.Errorf("wrapped: %w", &Error{Code: CodeInvalidSequence, Path: "event.sequence", Phase: DeliveryPrecommit, Err: inner})
	if code, ok := CodeOf(wrapped); !ok || code != CodeInvalidSequence || !errors.Is(wrapped, inner) {
		t.Errorf("wrapped=%v", wrapped)
	}
	var typedNil *Error
	var err error = typedNil
	if code, ok := CodeOf(err); ok || code != "" || typedNil.Error() != "" || typedNil.Unwrap() != nil {
		t.Error("typed nil unsafe")
	}
	canary := "synthetic_delta_marker"
	if strings.Contains((&Error{Code: CodeCompletionMismatch, Path: "item.content", Err: errors.New("values differ")}).Error(), canary) {
		t.Error("error leaked content")
	}
}
