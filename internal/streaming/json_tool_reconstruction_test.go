package streaming

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestMachineReconstructsJSONContent(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	itemStart, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	b.sequence++
	b.mustAccept(itemStart)
	contentStart, _ := canonical.NewContentStartedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, canonical.ContentType("json"))
	b.sequence++
	b.mustAccept(contentStart)
	for _, fragment := range []string{`{"b":`, `2,"a":1}`} {
		delta, _ := canonical.NewJSONDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, fragment)
		b.sequence++
		b.mustAccept(delta)
	}
	value, _ := canonicaljson.Parse([]byte(`{"a":1,"b":2}`), canonicaljson.DefaultLimits())
	jsonContent, _ := canonical.NewJSONContent(value)
	block, _ := canonical.NewJSONBlock(jsonContent)
	completed, _ := canonical.NewContentCompletedEvent(b.nextEventID(), b.responseID, b.sequence, itemID, 0, block)
	if _, err := b.machine.Accept(completed); err != nil {
		t.Fatalf("content completion failed: %v", err)
	}
}

func TestMachineReconstructsNamespacedToolArguments(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID, callID := streamItemID(t, 9), streamCallID(t, 9)
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("tool_call"), CallID: callID, Namespace: "clock", Name: "curr_time"})
	b.sequence++
	b.mustAccept(start)
	delta, _ := canonical.NewToolArgumentsDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, `{}`)
	b.sequence++
	b.mustAccept(delta)
	arguments, _ := canonicaljson.Parse([]byte(`{}`), canonicaljson.DefaultLimits())
	call, _ := canonical.NewNamespacedToolCall(itemID, callID, "clock", "curr_time", arguments)
	item, _ := canonical.NewToolCallItem(call)
	completed, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	if _, err := b.machine.Accept(completed); err != nil {
		t.Fatalf("namespaced completion failed: %v", err)
	}
}

func TestMachineReconstructsToolArguments(t *testing.T) {
	b := newStreamBuilder(t, DefaultMachineOptions())
	itemID := streamItemID(t, 1)
	callID := streamCallID(t, 1)
	start, _ := canonical.NewItemStartedEvent(b.nextEventID(), b.responseID, b.sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("tool_call"), CallID: callID, Name: "Search"})
	b.sequence++
	b.mustAccept(start)
	for index, fragment := range []string{`{"query":`, `"hello"}`} {
		delta, _ := canonical.NewToolArgumentsDeltaEvent(b.nextEventID(), b.responseID, b.sequence, itemID, fragment)
		b.sequence++
		transition := b.mustAccept(delta)
		if !transition.SemanticOutput() || transition.JustCommitted() != (index == 0) {
			t.Errorf("tool delta %d transition=%#v", index, transition)
		}
	}
	arguments, _ := canonicaljson.Parse([]byte(`{"query":"hello"}`), canonicaljson.DefaultLimits())
	call, _ := canonical.NewToolCall(itemID, callID, "Search", arguments)
	item, _ := canonical.NewToolCallItem(call)
	completed, _ := canonical.NewItemCompletedEvent(b.nextEventID(), b.responseID, b.sequence, 0, item)
	transition := b.mustAccept(completed)
	if transition.SemanticOutput() || transition.JustCommitted() {
		t.Errorf("tool completion transition=%#v", transition)
	}
}
