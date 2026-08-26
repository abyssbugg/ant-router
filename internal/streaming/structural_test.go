package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestMachineInterleavedItemAndContentStarts(t *testing.T) {
	machine, _ := NewMachine(DefaultMachineOptions())
	responseID := streamResponseID(t, 1)
	start, _ := canonical.NewResponseStartedEvent(streamEventID(t, 1), responseID, 0, "model")
	_, _ = machine.Accept(start)
	messageID := streamItemID(t, 1)
	toolID := streamItemID(t, 2)
	reasoningID := streamItemID(t, 3)
	events := []canonical.Event{}
	message, _ := canonical.NewItemStartedEvent(streamEventID(t, 2), responseID, 1, canonical.ItemStartParams{Index: 0, ID: messageID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	tool, _ := canonical.NewItemStartedEvent(streamEventID(t, 3), responseID, 2, canonical.ItemStartParams{Index: 1, ID: toolID, Type: canonical.ItemType("tool_call"), CallID: streamCallID(t, 1), Name: "Search"})
	reasoning, _ := canonical.NewItemStartedEvent(streamEventID(t, 4), responseID, 3, canonical.ItemStartParams{Index: 2, ID: reasoningID, Type: canonical.ItemType("reasoning_summary")})
	messageContent, _ := canonical.NewContentStartedEvent(streamEventID(t, 5), responseID, 4, messageID, 0, canonical.ContentType("text"))
	reasoningContent, _ := canonical.NewContentStartedEvent(streamEventID(t, 6), responseID, 5, reasoningID, 0, canonical.ContentType("text"))
	events = append(events, message, tool, reasoning, messageContent, reasoningContent)
	for _, event := range events {
		if !event.Valid() {
			t.Fatalf("test fixture %q is invalid", event.Type())
		}
		transition, err := machine.Accept(event)
		if err != nil || transition.SemanticOutput() {
			t.Fatalf("Accept(%q)=%#v,%v", event.Type(), transition, err)
		}
	}
	snapshot := machine.Snapshot()
	if snapshot.StartedItems() != 3 || snapshot.OpenContent() != 2 || snapshot.NextSequence() != 6 || snapshot.State() != StatePrecommit {
		t.Errorf("snapshot=%#v", snapshot)
	}
	if len(machine.eventIDs) != 6 || len(machine.items) != 3 || len(machine.itemOrder) != 3 || machine.items[messageID].nextContent != 1 || machine.items[reasoningID].nextContent != 1 {
		t.Errorf("internal structural state is inconsistent")
	}
}

func TestMachineStructuralRejectionsAreTransactional(t *testing.T) {
	machine, _ := NewMachine(DefaultMachineOptions())
	responseID := streamResponseID(t, 1)
	start, _ := canonical.NewResponseStartedEvent(streamEventID(t, 1), responseID, 0, "model")
	_, _ = machine.Accept(start)
	messageID := streamItemID(t, 1)
	message, _ := canonical.NewItemStartedEvent(streamEventID(t, 2), responseID, 1, canonical.ItemStartParams{Index: 0, ID: messageID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	_, _ = machine.Accept(message)

	cases := []canonical.Event{}
	wrongSequence, _ := canonical.NewContentStartedEvent(streamEventID(t, 3), responseID, 9, messageID, 0, canonical.ContentType("text"))
	wrongResponse, _ := canonical.NewContentStartedEvent(streamEventID(t, 4), streamResponseID(t, 9), 2, messageID, 0, canonical.ContentType("text"))
	duplicateEvent, _ := canonical.NewContentStartedEvent(streamEventID(t, 2), responseID, 2, messageID, 0, canonical.ContentType("text"))
	duplicateItem, _ := canonical.NewItemStartedEvent(streamEventID(t, 6), responseID, 2, canonical.ItemStartParams{Index: 1, ID: messageID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	contentGap, _ := canonical.NewContentStartedEvent(streamEventID(t, 7), responseID, 2, messageID, 1, canonical.ContentType("text"))
	missingItem, _ := canonical.NewContentStartedEvent(streamEventID(t, 8), responseID, 2, streamItemID(t, 8), 0, canonical.ContentType("text"))
	cases = append(cases, wrongSequence, wrongResponse, duplicateEvent, duplicateItem, contentGap, missingItem)
	for _, event := range cases {
		if !event.Valid() {
			t.Fatalf("test fixture %q is invalid", event.Type())
		}
		before := machine.Snapshot()
		_, err := machine.Accept(event)
		if err == nil {
			t.Errorf("event %q accepted", event.Type())
		}
		if event.ID() == message.ID() && codeOfStream(err) != CodeDuplicateEvent {
			t.Errorf("duplicate event code = %q", codeOfStream(err))
		}
		if machine.Snapshot() != before {
			t.Errorf("event %q mutated snapshot", event.Type())
		}
	}
}
