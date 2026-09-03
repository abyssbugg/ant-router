package streaming

import (
	"bytes"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func streamContinuation(t *testing.T, provider, model string, data []byte) canonical.Continuation {
	t.Helper()
	continuation, err := canonical.NewContinuation(provider, model, data)
	if err != nil {
		t.Fatalf("NewContinuation() failed: %v", err)
	}
	return continuation
}

func continuationEvent(t *testing.T, b *streamBuilder, continuation canonical.Continuation) canonical.Event {
	t.Helper()
	event, err := canonical.NewContinuationUpdatedEvent(b.nextEventID(), b.responseID, b.sequence, continuation)
	if err != nil {
		t.Fatalf("NewContinuationUpdatedEvent() failed: %v", err)
	}
	b.sequence++
	return event
}

func TestMachineContinuationIsNonsemanticAndAttachedToResponse(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	continuation := streamContinuation(t, "openai-primary", "gpt-5.2", []byte{1, 2, 3})
	transition := b.mustAccept(continuationEvent(t, b, continuation))
	if transition.SemanticOutput() || transition.JustCommitted() || b.machine.State() != StatePrecommit {
		t.Fatalf("continuation transition=%#v state=%q", transition, b.machine.State())
	}
	snapshot := b.machine.Snapshot()
	if !snapshot.HasContinuation() || snapshot.ContinuationBytes() != 3 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	completed, err := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("content_filter"), canonical.Usage{}, nil)
	if err != nil {
		t.Fatalf("NewResponseCompletedEvent() failed: %v", err)
	}
	terminal := b.mustAccept(completed)
	response, ok := terminal.Response()
	if !ok {
		t.Fatal("terminal response missing")
	}
	got, ok := response.Continuation()
	if !ok || !bytes.Equal(got.Bytes(), continuation.Bytes()) {
		t.Fatalf("response continuation=%#v, %v", got, ok)
	}
}

func TestMachineContinuationUpdatesAreCumulativeAndAffine(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	first := streamContinuation(t, "openai-primary", "gpt-5.2", []byte{1})
	b.mustAccept(continuationEvent(t, b, first))
	before := b.machine.Snapshot()

	duplicate, err := canonical.NewContinuationUpdatedEvent(b.nextEventID(), b.responseID, b.sequence, first)
	if err != nil {
		t.Fatalf("duplicate event construction failed: %v", err)
	}
	if _, err := b.machine.Accept(duplicate); codeOfStream(err) != CodeInvalidItemState {
		t.Fatalf("duplicate continuation error=%v", err)
	}
	if b.machine.Snapshot() != before {
		t.Fatal("duplicate continuation mutated machine")
	}

	wrongProvider := streamContinuation(t, "openai-secondary", "gpt-5.2", []byte{2})
	mismatch, _ := canonical.NewContinuationUpdatedEvent(b.nextEventID(), b.responseID, b.sequence, wrongProvider)
	if _, err := b.machine.Accept(mismatch); codeOfStream(err) != CodeInvalidItemState {
		t.Fatalf("provider mismatch error=%v", err)
	}
	if b.machine.Snapshot() != before {
		t.Fatal("provider mismatch mutated machine")
	}

	second := streamContinuation(t, "openai-primary", "gpt-5.2", []byte{1, 2})
	b.mustAccept(continuationEvent(t, b, second))
	if snapshot := b.machine.Snapshot(); !snapshot.HasContinuation() || snapshot.ContinuationBytes() != 2 || snapshot.Committed() {
		t.Fatalf("updated snapshot=%#v", snapshot)
	}
}

func TestMachineReconstructsEmptyReasoningWithContinuation(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	continuation := streamContinuation(t, "openai-primary", "gpt-5.2", []byte{1})
	b.mustAccept(continuationEvent(t, b, continuation))
	itemID := streamItemID(t, 9)
	started, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("reasoning_summary")})
	b.sequence++
	b.mustAccept(started)
	reasoning, _ := canonical.NewReasoningSummary(itemID, nil)
	item, _ := canonical.NewReasoningSummaryItem(reasoning)
	itemCompleted, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	b.sequence++
	b.mustAccept(itemCompleted)
	completed, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("stop"), canonical.Usage{}, []canonical.ItemID{itemID})
	terminal := b.mustAccept(completed)
	response, ok := terminal.Response()
	if !ok || len(response.Items()) != 1 {
		t.Fatalf("response=%#v, %v", response, ok)
	}
	got, ok := response.Continuation()
	if !ok || !bytes.Equal(got.Bytes(), continuation.Bytes()) {
		t.Fatalf("response continuation=%#v, %v", got, ok)
	}
}

func TestMachineRejectsContinuationBeforeResponseAndAfterTerminal(t *testing.T) {
	machine, err := NewMachine(DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	continuation := streamContinuation(t, "openai-primary", "gpt-5.2", []byte{1})
	before, _ := canonical.NewContinuationUpdatedEvent(streamEventID(t, 1), streamResponseID(t, 1), 0, continuation)
	if _, err := machine.Accept(before); codeOfStream(err) != CodeInvalidSequence {
		t.Fatalf("continuation before response error=%v", err)
	}

	b := newStreamBuilder(t, DefaultMachineOptions())
	completed, _ := canonical.NewResponseCompletedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.FinishReason("content_filter"), canonical.Usage{}, nil)
	b.sequence++
	b.mustAccept(completed)
	after, _ := canonical.NewContinuationUpdatedEvent(b.nextEventID(), b.responseID, b.sequence, continuation)
	if _, err := b.machine.Accept(after); codeOfStream(err) != CodeEventAfterTerminal {
		t.Fatalf("continuation after terminal error=%v", err)
	}
}
