package canonical

import (
	"strings"
	"testing"
)

func TestEventEnvelopeAndDeltaRejections(t *testing.T) {
	eventID := testEventID(t, 1)
	responseID := testResponseID(t, 1)
	itemID := testItemID(t, 1)
	if _, err := NewResponseStartedEvent("", responseID, 0, "model"); codeOfCanonical(err) != CodeInvalidID {
		t.Error("invalid event ID accepted")
	}
	if _, err := NewResponseStartedEvent(eventID, "", 0, "model"); codeOfCanonical(err) != CodeInvalidID {
		t.Error("invalid response ID accepted")
	}
	if _, err := NewResponseStartedEvent(eventID, responseID, 0, ""); err == nil {
		t.Error("empty model accepted")
	}
	if _, err := NewTextDeltaEvent(eventID, responseID, 0, itemID, 0, ""); err == nil {
		t.Error("empty text delta accepted")
	}
	if _, err := NewTextDeltaEvent(eventID, responseID, 0, itemID, 0, string([]byte{0xff})); codeOfCanonical(err) != CodeInvalidUTF8 {
		t.Error("invalid UTF-8 delta accepted")
	}
	if _, err := NewJSONDeltaEvent(eventID, responseID, 0, itemID, 0, strings.Repeat("x", MaxOrdinaryEventBytes+1)); codeOfCanonical(err) != CodeLimitExceeded {
		t.Error("oversized JSON delta accepted")
	}
	if _, err := NewContentStartedEvent(eventID, responseID, 0, itemID, -1, ContentType("text")); err == nil {
		t.Error("negative content index accepted")
	}
}

func TestItemStartedRejections(t *testing.T) {
	base := ItemStartParams{Index: 0, ID: testItemID(t, 1), Type: ItemType("message"), Role: Role("assistant")}
	cases := []ItemStartParams{
		{Index: -1, ID: base.ID, Type: base.Type, Role: base.Role},
		{Index: MaxResponseItems, ID: base.ID, Type: base.Type, Role: base.Role},
		{Index: 0, ID: "", Type: base.Type, Role: base.Role},
		{Index: 0, ID: base.ID, Type: ItemType("tool_result")},
		{Index: 0, ID: base.ID, Type: ItemType("message"), Role: Role("user")},
		{Index: 0, ID: base.ID, Type: ItemType("tool_call"), CallID: "", Name: "Search"},
		{Index: 0, ID: base.ID, Type: ItemType("tool_call"), CallID: testCallID(t, 1), Name: "bad name"},
		{Index: 0, ID: base.ID, Type: ItemType("reasoning_summary"), Name: "unexpected"},
	}
	for index, params := range cases {
		if _, err := NewItemStartedEvent(testEventID(t, 1), testResponseID(t, 1), 0, params); err == nil {
			t.Errorf("case %d accepted: %#v", index, params)
		}
	}
}

func TestCompletedAndTerminalEventRejections(t *testing.T) {
	eventID := testEventID(t, 1)
	responseID := testResponseID(t, 1)
	itemID := testItemID(t, 1)
	if _, err := NewContentCompletedEvent(eventID, responseID, 0, itemID, 0, ContentBlock{}); err == nil {
		t.Error("invalid completed content accepted")
	}
	result, _ := NewToolResult(itemID, testCallID(t, 1), []ContentBlock{testTextBlock(t, "result")}, false)
	resultItem, _ := NewToolResultItem(result)
	if _, err := NewItemCompletedEvent(eventID, responseID, 0, 0, resultItem); err == nil {
		t.Error("tool result output accepted")
	}
	usage := Usage{}
	if _, err := NewResponseCompletedEvent(eventID, responseID, 0, FinishReason("cancelled"), usage, nil); err == nil {
		t.Error("cancelled completion accepted")
	}
	if _, err := NewResponseCompletedEvent(eventID, responseID, 0, FinishReason("stop"), usage, []ItemID{itemID, itemID}); err == nil {
		t.Error("duplicate terminal item IDs accepted")
	}
	cancelFailure, _ := NewFailure(FailureParams{Code: FailureCode("cancelled"), Message: "cancelled"})
	if _, err := NewResponseFailedEvent(eventID, responseID, 0, cancelFailure, nil); err == nil {
		t.Error("cancelled failure accepted as failed event")
	}
	internal, _ := NewFailure(FailureParams{Code: FailureCode("internal_error"), Message: "internal", Retryable: true})
	if _, err := NewResponseCancelledEvent(eventID, responseID, 0, internal, nil); err == nil {
		t.Error("non-cancelled failure accepted as cancelled event")
	}
}

func TestZeroEventInvalid(t *testing.T) {
	if (Event{}).Valid() {
		t.Error("zero Event is valid")
	}
}
