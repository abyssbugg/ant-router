package streaming

import (
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// Result is the immutable terminal outcome and safe metrics of a pump run.
type Result struct {
	phase       DeliveryPhase
	terminal    TerminalKind
	response    canonical.Response
	hasResponse bool
	failure     canonical.Failure
	hasFailure  bool
	usage       canonical.Usage
	hasUsage    bool
	received    int
	delivered   int
	peakEvents  int
	peakBytes   int
	cause       error
}

func newResult(phase DeliveryPhase, terminal TerminalKind, response *canonical.Response, failure *canonical.Failure, usage *canonical.Usage, received, delivered, peakEvents, peakBytes int, cause error) (Result, error) {
	result := Result{phase: phase, terminal: terminal, received: received, delivered: delivered, peakEvents: peakEvents, peakBytes: peakBytes, cause: cause}
	if response != nil {
		result.response, result.hasResponse = *response, true
	}
	if failure != nil {
		result.failure, result.hasFailure = *failure, true
	}
	if usage != nil {
		result.usage, result.hasUsage = *usage, true
	}
	if !result.Valid() {
		return Result{}, &Error{Code: CodeTerminalMismatch, Path: "result.metrics", Phase: phase, Err: errors.New("result is invalid")}
	}
	return result, nil
}

// Valid reports whether phase, terminal values, counters, and optional values are consistent.
func (r Result) Valid() bool {
	if !r.phase.Valid() || !r.terminal.Valid() || r.received < 0 || r.delivered < 0 || r.peakEvents < 0 || r.peakBytes < 0 || r.delivered > r.received {
		return false
	}
	if r.hasResponse && !r.response.Valid() || r.hasFailure && !r.failure.Valid() || r.hasUsage && !r.usage.Valid() {
		return false
	}
	switch r.terminal {
	case TerminalCompleted:
		return r.hasResponse && !r.hasFailure
	case TerminalFailed:
		return !r.hasResponse && r.hasFailure && r.failure.Code() != canonical.FailureCode("cancelled")
	case TerminalCancelled:
		return !r.hasResponse && r.hasFailure && r.failure.Code() == canonical.FailureCode("cancelled")
	case TerminalNone:
		return !r.hasResponse && !r.hasFailure
	default:
		return false
	}
}

// Phase returns downstream visibility at termination.
func (r Result) Phase() DeliveryPhase { return r.phase }

// TerminalKind returns the terminal outcome.
func (r Result) TerminalKind() TerminalKind { return r.terminal }

// Response returns the completed response when present.
func (r Result) Response() (canonical.Response, bool) { return r.response, r.hasResponse }

// Failure returns the canonical failure when present.
func (r Result) Failure() (canonical.Failure, bool) { return r.failure, r.hasFailure }

// Usage returns final usage when present.
func (r Result) Usage() (canonical.Usage, bool) { return r.usage, r.hasUsage }

// ReceivedEvents returns accepted source event count.
func (r Result) ReceivedEvents() int { return r.received }

// DeliveredEvents returns attempted successful sink delivery count.
func (r Result) DeliveredEvents() int { return r.delivered }

// PeakPrecommitEvents returns the maximum buffered event count.
func (r Result) PeakPrecommitEvents() int { return r.peakEvents }

// PeakPrecommitBytes returns the maximum buffered encoded bytes.
func (r Result) PeakPrecommitBytes() int { return r.peakBytes }

// Cause returns the safe terminal runtime cause.
func (r Result) Cause() error { return r.cause }
