package streaming

import (
	"context"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestPumpClassifiesSourceEOFBeforeAndAfterCommit(t *testing.T) {
	events := loadGoldenEvents(t)
	cases := []struct {
		name         string
		sourceEvents []canonical.Event
		phase        DeliveryPhase
		delivered    int
	}{
		{name: "precommit", sourceEvents: events[:1], phase: DeliveryPrecommit, delivered: 0},
		{name: "postcommit", sourceEvents: events[:4], phase: DeliveryPostcommit, delivered: 4},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := &scriptedSource{events: test.sourceEvents}
			sink := &recordingSink{failAt: -1}
			result := newTestPump(t, source, sink, DefaultPumpOptions()).Run(context.Background())
			if resultCode(result) != CodeSourceFailed || result.Phase() != test.phase || result.DeliveredEvents() != test.delivered {
				t.Fatalf("result=%#v code=%q", result, resultCode(result))
			}
		})
	}
}

func TestPumpClassifiesSourceAndSinkFailures(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events[:1], terminal: errSource}
	sink := &recordingSink{failAt: -1}
	result := newTestPump(t, source, sink, DefaultPumpOptions()).Run(context.Background())
	if resultCode(result) != CodeSourceFailed || result.Phase() != DeliveryPrecommit {
		t.Fatalf("source result=%#v code=%q", result, resultCode(result))
	}

	for _, failAt := range []int{0, 2} {
		t.Run("sink", func(t *testing.T) {
			source := &scriptedSource{events: events}
			sink := &recordingSink{failAt: failAt, err: errSink}
			result := newTestPump(t, source, sink, DefaultPumpOptions()).Run(context.Background())
			if resultCode(result) != CodeSinkFailed || result.Phase() != DeliveryPostcommit || result.DeliveredEvents() != failAt || result.ReceivedEvents() != 4 {
				t.Fatalf("failAt=%d result=%#v code=%q", failAt, result, resultCode(result))
			}
		})
	}
}

func TestPumpReportsBufferLimitWithoutDelivery(t *testing.T) {
	events := loadGoldenEvents(t)
	options := DefaultPumpOptions()
	options.MaxPrecommitEvents = 2
	source := &scriptedSource{events: events}
	sink := &recordingSink{failAt: -1}
	result := newTestPump(t, source, sink, options).Run(context.Background())
	if resultCode(result) != CodePrecommitLimitExceeded || result.Phase() != DeliveryPrecommit || result.ReceivedEvents() != 3 || result.DeliveredEvents() != 0 || len(sink.snapshot()) != 0 {
		t.Fatalf("result=%#v code=%q", result, resultCode(result))
	}
}

func TestPumpPreservesCompletedResponseWhenSourceCloseFails(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events, closeError: errClose}
	sink := &recordingSink{failAt: -1}
	result := newTestPump(t, source, sink, DefaultPumpOptions()).Run(context.Background())
	if resultCode(result) != CodeCloseFailed || result.TerminalKind() != TerminalCompleted || result.Phase() != DeliveryPostcommit || source.closed.Load() != 1 {
		t.Fatalf("result=%#v code=%q closed=%d", result, resultCode(result), source.closed.Load())
	}
	if _, ok := result.Response(); !ok {
		t.Fatal("completed response lost")
	}
}
