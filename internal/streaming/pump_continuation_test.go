package streaming

import (
	"context"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func pumpContinuationEvent(t *testing.T, responseID canonical.ResponseID, sequence uint64, value byte) canonical.Event {
	t.Helper()
	continuation := streamContinuation(t, "openai-primary", "gpt-5.2", []byte{value})
	event, err := canonical.NewContinuationUpdatedEvent(streamEventID(t, byte(sequence+2)), responseID, sequence, continuation)
	if err != nil {
		t.Fatalf("NewContinuationUpdatedEvent() failed: %v", err)
	}
	return event
}

func TestPumpBuffersAndAttachesPrecommitContinuation(t *testing.T) {
	responseID := streamResponseID(t, 1)
	started := newResponseStartedForPump(t, responseID)
	updated := pumpContinuationEvent(t, responseID, 1, 7)
	completed, err := canonical.NewResponseCompletedEvent(streamEventID(t, 4), responseID, 2, canonical.FinishReason("content_filter"), canonical.Usage{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{failAt: -1}
	pump := newTestPump(t, &scriptedSource{events: []canonical.Event{started, updated, completed}}, sink, DefaultPumpOptions())
	result := pump.Run(context.Background())
	if result.TerminalKind() != TerminalCompleted || result.Phase() != DeliveryPostcommit || result.DeliveredEvents() != 3 {
		t.Fatalf("result=%#v", result)
	}
	response, ok := result.Response()
	if !ok {
		t.Fatal("completed response missing")
	}
	continuation, ok := response.Continuation()
	if !ok || continuation.Bytes()[0] != 7 {
		t.Fatalf("response continuation=%#v, %v", continuation, ok)
	}
	events := sink.snapshot()
	if len(events) != 3 || events[0].Type() != canonical.EventType("response_started") || events[1].Type() != canonical.EventType("continuation_updated") || events[2].Type() != canonical.EventType("response_completed") {
		t.Fatalf("delivered events=%#v", events)
	}
}

func TestPumpContinuationBufferLimitDoesNotAdvanceMachine(t *testing.T) {
	responseID := streamResponseID(t, 1)
	machine, err := NewMachine(DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	options := DefaultPumpOptions()
	options.MaxPrecommitEvents = 1
	source := &scriptedSource{events: []canonical.Event{newResponseStartedForPump(t, responseID), pumpContinuationEvent(t, responseID, 1, 7)}}
	pump, err := NewPump(machine, source, &recordingSink{failAt: -1}, options)
	if err != nil {
		t.Fatal(err)
	}
	result := pump.Run(context.Background())
	if resultCode(result) != CodePrecommitLimitExceeded || result.Phase() != DeliveryPrecommit {
		t.Fatalf("result=%#v code=%q", result, resultCode(result))
	}
	snapshot := machine.Snapshot()
	if snapshot.NextSequence() != 1 || snapshot.HasContinuation() {
		t.Fatalf("machine advanced past rejected continuation: %#v", snapshot)
	}
}

func TestPumpDeliversPostcommitContinuationLosslessly(t *testing.T) {
	responseID := streamResponseID(t, 1)
	itemID := streamItemID(t, 1)
	started := newResponseStartedForPump(t, responseID)
	itemStarted, _ := canonical.NewItemStartedEvent(streamEventID(t, 2), responseID, 1, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	contentStarted, _ := canonical.NewContentStartedEvent(streamEventID(t, 3), responseID, 2, itemID, 0, canonical.ContentType("text"))
	delta, _ := canonical.NewTextDeltaEvent(streamEventID(t, 4), responseID, 3, itemID, 0, "x")
	updated := pumpContinuationEvent(t, responseID, 4, 8)
	machine, _ := NewMachine(DefaultMachineOptions())
	sink := &recordingSink{failAt: -1}
	pump, err := NewPump(machine, &scriptedSource{events: []canonical.Event{started, itemStarted, contentStarted, delta, updated}}, sink, DefaultPumpOptions())
	if err != nil {
		t.Fatal(err)
	}
	result := pump.Run(context.Background())
	if resultCode(result) != CodeSourceFailed || result.Phase() != DeliveryPostcommit {
		t.Fatalf("result=%#v code=%q", result, resultCode(result))
	}
	if snapshot := machine.Snapshot(); !snapshot.HasContinuation() || snapshot.ContinuationBytes() != 1 || !snapshot.Committed() {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	events := sink.snapshot()
	if len(events) != 5 || events[4].Type() != canonical.EventType("continuation_updated") {
		t.Fatalf("delivered events=%#v", events)
	}
}
