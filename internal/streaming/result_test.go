package streaming

import (
	"errors"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestResultAccessors(t *testing.T) {
	response := simpleResponse(t)
	failure := simpleFailure(t)
	usage, _ := canonical.NewUsage(canonical.UsageParams{})
	cause := errors.New("safe cause")
	result, err := newResult(DeliveryPostcommit, TerminalCompleted, &response, nil, &usage, 10, 9, 5, 100, cause)
	if err != nil {
		t.Fatalf("newResult() failed: %v", err)
	}
	if !result.Valid() || result.Phase() != DeliveryPostcommit || result.TerminalKind() != TerminalCompleted || result.ReceivedEvents() != 10 || result.DeliveredEvents() != 9 || result.PeakPrecommitEvents() != 5 || result.PeakPrecommitBytes() != 100 || !errors.Is(result.Cause(), cause) {
		t.Errorf("result=%#v", result)
	}
	if _, ok := result.Response(); !ok {
		t.Error("response missing")
	}
	if _, ok := result.Failure(); ok {
		t.Error("unexpected failure")
	}
	failed, err := newResult(DeliveryPostcommit, TerminalFailed, nil, &failure, &usage, 10, 9, 5, 100, cause)
	if err != nil {
		t.Fatalf("failed newResult() failed: %v", err)
	}
	if _, ok := failed.Failure(); !ok {
		t.Error("failure missing")
	}
	if _, ok := result.Usage(); !ok {
		t.Error("usage missing")
	}
}

func TestResultRejectsNegativeOrImpossibleCounters(t *testing.T) {
	cases := [][4]int{{-1, 0, 0, 0}, {0, -1, 0, 0}, {0, 0, -1, 0}, {0, 0, 0, -1}, {1, 2, 0, 0}}
	for _, counters := range cases {
		_, err := newResult(DeliveryPrecommit, TerminalNone, nil, nil, nil, counters[0], counters[1], counters[2], counters[3], nil)
		code, _ := CodeOf(err)
		if code != CodeTerminalMismatch {
			t.Errorf("counters %v code = %q", counters, code)
		}
	}
}
