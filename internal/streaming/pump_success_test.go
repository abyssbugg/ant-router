package streaming

import (
	"context"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestPumpDeliversGoldenStreamInOrder(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events}
	sink := &recordingSink{failAt: -1}
	result := newTestPump(t, source, sink, DefaultPumpOptions()).Run(context.Background())
	if !result.Valid() || result.Phase() != DeliveryPostcommit || result.TerminalKind() != TerminalCompleted || result.ReceivedEvents() != len(events) || result.DeliveredEvents() != len(events) || source.closed.Load() != 1 {
		t.Fatalf("result=%#v closed=%d", result, source.closed.Load())
	}
	response, ok := result.Response()
	if !ok || !response.Valid() || len(response.Items()) != 2 {
		t.Fatalf("response=%#v ok=%v", response, ok)
	}
	delivered := sink.snapshot()
	if len(delivered) != len(events) {
		t.Fatalf("delivered=%d want=%d", len(delivered), len(events))
	}
	for index := range events {
		if delivered[index].ID() != events[index].ID() {
			t.Fatalf("event %d out of order", index)
		}
	}
	if result.PeakPrecommitEvents() != 4 || result.PeakPrecommitBytes() <= 0 {
		t.Errorf("peaks events=%d bytes=%d", result.PeakPrecommitEvents(), result.PeakPrecommitBytes())
	}
}

func TestPumpSuppressesPrecommitFailedTerminal(t *testing.T) {
	builder := newStreamBuilder(t, DefaultMachineOptions())
	failure := simpleFailure(t)
	failed := newFailedEventForPump(t, builder.responseID, 1, failure)
	events := []canonical.Event{newResponseStartedForPump(t, builder.responseID), failed}
	source := &scriptedSource{events: events}
	sink := &recordingSink{failAt: -1}
	result := newTestPump(t, source, sink, DefaultPumpOptions()).Run(context.Background())
	if result.Phase() != DeliveryPrecommit || result.TerminalKind() != TerminalFailed || result.DeliveredEvents() != 0 || len(sink.snapshot()) != 0 {
		t.Fatalf("result=%#v delivered=%d", result, len(sink.snapshot()))
	}
	if _, ok := result.Failure(); !ok {
		t.Fatal("failure missing")
	}
}

func TestPumpCommitsAndDeliversEmptyCompletedResponse(t *testing.T) {
	responseID := streamResponseID(t, 1)
	start := newResponseStartedForPump(t, responseID)
	completed := newEmptyCompletedForPump(t, responseID, 1)
	source := &scriptedSource{events: []canonical.Event{start, completed}}
	sink := &recordingSink{failAt: -1}
	result := newTestPump(t, source, sink, DefaultPumpOptions()).Run(context.Background())
	if result.Phase() != DeliveryPostcommit || result.TerminalKind() != TerminalCompleted || result.DeliveredEvents() != 2 || len(sink.snapshot()) != 2 {
		t.Fatalf("result=%#v", result)
	}
}
