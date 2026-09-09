package canonical

import (
	"strings"
	"testing"
)

func TestCustomCallEventVariants(t *testing.T) {
	eventID := testEventID(t, 97)
	responseID := testResponseID(t, 97)
	itemID := testItemID(t, 97)
	callID := testCallID(t, 97)
	started, err := NewItemStartedEvent(eventID, responseID, 0, ItemStartParams{Index: 0, ID: itemID, Type: ItemType("custom_tool_call"), CallID: callID, Namespace: "workspace", Name: "apply_patch"})
	if err != nil || !started.Valid() {
		t.Fatalf("custom start = %#v, err = %v", started, err)
	}
	delta, err := NewCustomInputDeltaEvent(eventID, responseID, 1, itemID, "*** Begin")
	if err != nil || !delta.Valid() || delta.Type() != EventType("custom_input_delta") {
		t.Fatalf("custom delta = %#v, err = %v", delta, err)
	}
	if payload, ok := delta.CustomInputDelta(); !ok || payload.ItemID() != itemID || payload.Delta() != "*** Begin" {
		t.Fatalf("custom delta payload = %#v, %v", payload, ok)
	}
	completed, err := NewCustomInputCompletedEvent(eventID, responseID, 2, itemID, "*** Begin Patch\n*** End Patch")
	if err != nil || !completed.Valid() || completed.Type() != EventType("custom_input_completed") {
		t.Fatalf("custom input completed = %#v, err = %v", completed, err)
	}
	if payload, ok := completed.CustomInputCompleted(); !ok || payload.ItemID() != itemID || payload.Input() == "" {
		t.Fatalf("custom completion payload = %#v, %v", payload, ok)
	}
	call, _ := NewNamespacedCustomToolCall(itemID, callID, "workspace", "apply_patch", "*** Begin Patch\n*** End Patch")
	item, _ := NewCustomToolCallItem(call)
	itemCompleted, err := NewItemCompletedEvent(eventID, responseID, 3, 0, item)
	if err != nil || !itemCompleted.Valid() {
		t.Fatalf("custom item completed = %#v, err = %v", itemCompleted, err)
	}
}

func TestCustomCallEventsRejectInvalid(t *testing.T) {
	eventID := testEventID(t, 98)
	responseID := testResponseID(t, 98)
	itemID := testItemID(t, 98)
	callID := testCallID(t, 98)
	if _, err := NewItemStartedEvent(eventID, responseID, 0, ItemStartParams{Index: 0, ID: itemID, Type: ItemType("custom_tool_call"), Role: Role("assistant"), CallID: callID, Name: "edit"}); err == nil {
		t.Fatal("custom start with role accepted")
	}
	if _, err := NewCustomInputDeltaEvent(eventID, responseID, 0, itemID, ""); err == nil {
		t.Fatal("empty custom input delta accepted")
	}
	if _, err := NewCustomInputDeltaEvent(eventID, responseID, 0, itemID, string([]byte{0xff})); codeOfCanonical(err) != CodeInvalidUTF8 {
		t.Fatal("invalid UTF-8 custom input delta accepted")
	}
	if _, err := NewCustomInputDeltaEvent(eventID, responseID, 0, itemID, "bad\x00input"); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Fatal("custom input delta control character accepted")
	}
	if _, err := NewCustomInputDeltaEvent(eventID, responseID, 0, itemID, strings.Repeat("x", MaxOrdinaryEventBytes+1)); codeOfCanonical(err) != CodeLimitExceeded {
		t.Fatal("oversized custom input delta accepted")
	}
	if _, err := NewCustomInputCompletedEvent(eventID, responseID, 0, itemID, string([]byte{0xff})); codeOfCanonical(err) != CodeInvalidUTF8 {
		t.Fatal("invalid UTF-8 completed custom input accepted")
	}
	if _, err := NewCustomInputCompletedEvent(eventID, responseID, 0, itemID, strings.Repeat("x", MaxCustomToolInputBytes+1)); codeOfCanonical(err) != CodeLimitExceeded {
		t.Fatal("oversized completed custom input accepted")
	}
}

func TestCustomCallErrorsRedactInput(t *testing.T) {
	const secret = "custom-secret-6947219efdb84ae5"
	_, err := NewCustomInputDeltaEvent(testEventID(t, 99), testResponseID(t, 99), 0, testItemID(t, 99), secret+"\x00")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("custom input leaked in error: %v", err)
	}
}

func FuzzCustomInputEvents(f *testing.F) {
	f.Add("delta")
	f.Add("bad\x00input")
	f.Add("")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > MaxCustomToolInputBytes+1 {
			return
		}
		itemID := testItemID(t, 102)
		if input != "" && len(input) <= MaxOrdinaryEventBytes {
			event, err := NewCustomInputDeltaEvent(testEventID(t, 102), testResponseID(t, 102), 0, itemID, input)
			if err == nil && !event.Valid() {
				t.Fatal("delta constructor returned invalid event")
			}
		}
		event, err := NewCustomInputCompletedEvent(testEventID(t, 103), testResponseID(t, 103), 0, itemID, input)
		if err == nil && !event.Valid() {
			t.Fatal("completion constructor returned invalid event")
		}
	})
}
