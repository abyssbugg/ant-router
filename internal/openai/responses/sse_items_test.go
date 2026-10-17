package responses

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func sseTestEventID(t *testing.T, seed byte) canonical.EventID {
	t.Helper()
	id, err := canonical.GenerateEventID(bytes.NewReader(bytes.Repeat([]byte{seed}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStartedOutputItemRejectsUnsupportedV2Items(t *testing.T) {
	responseID, err := canonical.GenerateResponseID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	itemID, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	callID, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	event, err := canonical.NewItemStartedEvent(sseTestEventID(t, 9), responseID, 1, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("custom_tool_call"), CallID: callID, Name: "tool"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := event.ItemStarted()
	if _, err := startedOutputItem(payload, nil); err == nil {
		t.Fatal("custom_tool_call item_started must fail closed instead of mapping to a reasoning item")
	}
}

func TestSSESinkRejectsCustomToolCallItemStart(t *testing.T) {
	responseID, err := canonical.GenerateResponseID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	itemID, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	callID, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	started, err := canonical.NewResponseStartedEvent(sseTestEventID(t, 9), responseID, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	itemStart, err := canonical.NewItemStartedEvent(sseTestEventID(t, 10), responseID, 1, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("custom_tool_call"), CallID: callID, Name: "tool"})
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := NewEncoder(time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	sink, err := NewSSESink(&flushBuffer{}, encoder)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), started); err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), itemStart); err == nil {
		t.Fatal("sink accepted a custom_tool_call item_started as a reasoning item")
	}
}

// TestSSESinkIsTransactionalOnMapFailure verifies that when mapEvent fails,
// the sink state (machine sequence, item indexes, call IDs) is not mutated.
// This allows a retry of a fixed version of the same event to be accepted.
func TestSSESinkIsTransactionalOnMapFailure(t *testing.T) {
	responseID, err := canonical.GenerateResponseID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	itemID, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	callID, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{9}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}

	// Set up a sink with a response_started event
	started, err := canonical.NewResponseStartedEvent(sseTestEventID(t, 9), responseID, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := NewEncoder(time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	writer := &flushBuffer{}
	sink, err := NewSSESink(writer, encoder)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Send(context.Background(), started); err != nil {
		t.Fatal(err)
	}

	// Send a custom_tool_call item_started (sequence 1) - this should fail in mapEvent
	badItemStart, err := canonical.NewItemStartedEvent(sseTestEventID(t, 10), responseID, 1, canonical.ItemStartParams{
		Index:  0,
		ID:     itemID,
		Type:   canonical.ItemType("custom_tool_call"),
		CallID: callID,
		Name:   "tool",
	})
	if err != nil {
		t.Fatal(err)
	}

	initialOutput := writer.String()
	if err := sink.Send(context.Background(), badItemStart); err == nil {
		t.Fatal("expected Send to return error for unsupported item type, but it succeeded")
	}

	// After rejection, the output should not have changed (no frame was written)
	afterRejectionOutput := writer.String()
	if afterRejectionOutput != initialOutput {
		t.Fatalf("output changed after rejection: before=%q, after=%q", initialOutput, afterRejectionOutput)
	}

	// Now send a valid tool_call item_started with the same sequence number.
	// This should be accepted and succeed if the machine was not advanced.
	validItemStart, err := canonical.NewItemStartedEvent(sseTestEventID(t, 11), responseID, 1, canonical.ItemStartParams{
		Index:  0,
		ID:     itemID,
		Type:   canonical.ItemType("tool_call"),
		CallID: callID,
		Name:   "tool",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := sink.Send(context.Background(), validItemStart); err != nil {
		t.Fatalf("Send with valid tool_call failed after rejected custom_tool_call: %v", err)
	}

	// Verify a frame was written for the valid event
	finalOutput := writer.String()
	if finalOutput == afterRejectionOutput {
		t.Fatal("no output frame written for valid item_started event after rejection")
	}
	if !strings.Contains(finalOutput, "response.output_item.added") {
		t.Fatalf("expected 'response.output_item.added' frame in output, got: %s", finalOutput)
	}
}
