package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/testfixtures"
)

// TestMachineReplaysProtocolV2Fixture feeds the complete shared v2 event
// fixture through the machine and asserts the reconstructed response. Every
// event accepts transactionally and the terminal event yields the expected
// three items in fixture order.
func TestMachineReplaysProtocolV2Fixture(t *testing.T) {
	machine, err := NewMachine(DefaultMachineOptions())
	if err != nil {
		t.Fatalf("NewMachine: %v", err)
	}
	events := testfixtures.ProtocolV2Events(t)
	var terminal Transition
	for index, event := range events {
		transition, err := machine.Accept(event)
		if err != nil {
			t.Fatalf("event %d (%s): %v", index, event.Type(), err)
		}
		if index == 0 && transition.SemanticOutput() {
			t.Fatalf("response_started reported semantic output: %#v", transition)
		}
		if index == len(events)-1 {
			terminal = transition
		}
	}
	snapshot := machine.Snapshot()
	if snapshot.State() != StateTerminal || !snapshot.Committed() {
		t.Fatalf("snapshot = %#v, want terminal committed state", snapshot)
	}
	response, ok := terminal.Response()
	if !ok || !response.Valid() {
		t.Fatalf("terminal response = %#v, %v", response, ok)
	}
	expectations := testfixtures.ProtocolV2EventExpectations(t)
	items := response.Items()
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	for index, item := range items {
		if item.ID() != expectations.ItemIDs[index] {
			t.Fatalf("item %d = %s, want %s", index, item.ID(), expectations.ItemIDs[index])
		}
	}
	if search, searchOK := items[0].HostedSearchCall(); !searchOK || search.Status() != canonical.HostedSearchStatusCompleted {
		t.Fatalf("first item = %#v, %v", items[0], searchOK)
	}
	custom, customOK := items[1].CustomToolCall()
	if !customOK || custom.Input() != expectations.CustomInput || custom.Namespace() != "workspace" || custom.Name() != "apply_patch" || custom.CallID() != expectations.CustomCallID {
		t.Fatalf("custom item = %#v, %v", items[1], customOK)
	}
	message, messageOK := items[2].Message()
	if !messageOK || len(message.Content()) != 1 {
		t.Fatalf("message item = %#v, %v", items[2], messageOK)
	}
	text, textOK := message.Content()[0].Text()
	if !textOK || text.Text() != expectations.MessageText || len(text.Citations()) != 1 || text.Citations()[0].URL() != "https://example.com" {
		t.Fatalf("message text = %#v, %v", message.Content()[0], textOK)
	}
}
