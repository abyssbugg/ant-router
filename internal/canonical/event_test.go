package canonical

import (
	"testing"
)

func TestCanonicalEventVariants(t *testing.T) {
	eventID := testEventID(t, 1)
	responseID := testResponseID(t, 1)
	itemID := testItemID(t, 1)

	started, err := NewResponseStartedEvent(eventID, responseID, 0, "model")
	if err != nil || !started.Valid() || started.Type() != EventType("response_started") || started.Sequence() != 0 {
		t.Fatalf("ResponseStarted = %#v, %v", started, err)
	}
	if payload, ok := started.ResponseStarted(); !ok || payload.Model() != "model" {
		t.Errorf("ResponseStarted payload = %#v, %v", payload, ok)
	}

	itemStarted, err := NewItemStartedEvent(eventID, responseID, 1, ItemStartParams{Index: 0, ID: itemID, Type: ItemType("message"), Role: Role("assistant")})
	if err != nil || !itemStarted.Valid() {
		t.Fatalf("ItemStarted = %#v, %v", itemStarted, err)
	}

	contentStarted, _ := NewContentStartedEvent(eventID, responseID, 2, itemID, 0, ContentType("text"))
	textDelta, _ := NewTextDeltaEvent(eventID, responseID, 3, itemID, 0, "hel")
	jsonDelta, _ := NewJSONDeltaEvent(eventID, responseID, 4, itemID, 0, `{"a":`)
	argumentsDelta, _ := NewToolArgumentsDeltaEvent(eventID, responseID, 5, itemID, `{"q":`)
	for _, event := range []Event{contentStarted, textDelta, jsonDelta, argumentsDelta} {
		if !event.Valid() {
			t.Errorf("event %q invalid", event.Type())
		}
	}

	block := testTextBlock(t, "hello")
	contentCompleted, _ := NewContentCompletedEvent(eventID, responseID, 6, itemID, 0, block)
	message, _ := NewMessage(itemID, Role("assistant"), []ContentBlock{block})
	messageItem, _ := NewMessageItem(message)
	itemCompleted, _ := NewItemCompletedEvent(eventID, responseID, 7, 0, messageItem)
	usage, _ := NewUsage(UsageParams{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	usageUpdated, _ := NewUsageUpdatedEvent(eventID, responseID, 8, usage)
	completedIDs := []ItemID{itemID}
	completed, _ := NewResponseCompletedEvent(eventID, responseID, 9, FinishReason("stop"), usage, completedIDs)
	for _, event := range []Event{contentCompleted, itemCompleted, usageUpdated, completed} {
		if !event.Valid() {
			t.Errorf("event %q invalid", event.Type())
		}
	}
	completedIDs[0] = ""
	payload, _ := completed.ResponseCompleted()
	ids := payload.ItemIDs()
	ids[0] = ""
	if !payload.ItemIDs()[0].Valid() {
		t.Error("terminal item IDs were mutated")
	}

	failure, _ := NewFailure(FailureParams{Code: FailureCode("internal_error"), Message: "internal error", Retryable: true})
	failed, _ := NewResponseFailedEvent(eventID, responseID, 10, failure, &usage)
	cancelFailure, _ := NewFailure(FailureParams{Code: FailureCode("cancelled"), Message: "cancelled"})
	cancelled, _ := NewResponseCancelledEvent(eventID, responseID, 11, cancelFailure, nil)
	if !failed.Valid() || !cancelled.Valid() {
		t.Error("terminal failure events invalid")
	}
}

func TestToolCallItemStarted(t *testing.T) {
	event, err := NewItemStartedEvent(testEventID(t, 1), testResponseID(t, 1), 0, ItemStartParams{Index: 0, ID: testItemID(t, 1), Type: ItemType("tool_call"), CallID: testCallID(t, 1), Name: "Search"})
	if err != nil || !event.Valid() {
		t.Fatalf("tool item started = %#v, %v", event, err)
	}
	payload, _ := event.ItemStarted()
	if payload.Name() != "Search" || !payload.CallID().Valid() {
		t.Errorf("payload = %#v", payload)
	}
}
