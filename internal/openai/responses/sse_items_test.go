package responses

import (
	"bytes"
	"context"
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
