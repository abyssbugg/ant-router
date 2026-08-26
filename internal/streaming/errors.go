package streaming

import (
	"errors"
	"fmt"
)

// ErrorCode is a stable transactional-streaming error category.
type ErrorCode string

const (
	// CodeInvalidSequence reports an unexpected event sequence.
	CodeInvalidSequence ErrorCode = "invalid_sequence"
	// CodeResponseMismatch reports an event for another response.
	CodeResponseMismatch ErrorCode = "response_mismatch"
	// CodeDuplicateEvent reports a repeated event ID.
	CodeDuplicateEvent ErrorCode = "duplicate_event"
	// CodeInvalidItemState reports an invalid item transition.
	CodeInvalidItemState ErrorCode = "invalid_item_state"
	// CodeInvalidContentState reports an invalid content transition.
	CodeInvalidContentState ErrorCode = "invalid_content_state"
	// CodeCompletionMismatch reports streamed/final value disagreement.
	CodeCompletionMismatch ErrorCode = "completion_mismatch"
	// CodeUsageRegression reports non-monotonic usage.
	CodeUsageRegression ErrorCode = "usage_regression"
	// CodeTerminalMismatch reports an invalid terminal/result state.
	CodeTerminalMismatch ErrorCode = "terminal_mismatch"
	// CodeEventAfterTerminal reports input after termination.
	CodeEventAfterTerminal ErrorCode = "event_after_terminal"
	// CodeResponseLimitExceeded reports reconstructed response growth beyond limits.
	CodeResponseLimitExceeded ErrorCode = "response_limit_exceeded"
	// CodePrecommitLimitExceeded reports precommit buffer growth beyond limits.
	CodePrecommitLimitExceeded ErrorCode = "precommit_limit_exceeded"
	// CodeSourceFailed reports an upstream source failure.
	CodeSourceFailed ErrorCode = "source_failed"
	// CodeSinkFailed reports a downstream sink failure.
	CodeSinkFailed ErrorCode = "sink_failed"
	// CodeDownstreamStalled reports a sink stall deadline.
	CodeDownstreamStalled ErrorCode = "downstream_stalled"
	// CodeCancelled reports caller or source cancellation.
	CodeCancelled ErrorCode = "cancelled"
	// CodeCloseFailed reports source cleanup failure.
	CodeCloseFailed ErrorCode = "close_failed"
)

// Error carries a stable code, safe path, delivery phase, and wrapped safe cause.
type Error struct {
	Code  ErrorCode
	Path  string
	Phase DeliveryPhase
	Err   error
}

// Error returns a safe streaming error message.
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

// Unwrap returns the underlying safe cause.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CodeOf extracts a streaming error code through wrapped errors.
func CodeOf(err error) (ErrorCode, bool) {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code, true
	}
	return "", false
}
