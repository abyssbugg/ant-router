package canonical

import (
	"bytes"
	"testing"
)

func codecEvents(t *testing.T) []Event {
	t.Helper()
	eventID := testEventID(t, 1)
	responseID := testResponseID(t, 1)
	itemID := testItemID(t, 1)
	usage, _ := NewUsage(UsageParams{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	block := testTextBlock(t, "hello")
	message, _ := NewMessage(itemID, Role("assistant"), []ContentBlock{block})
	item, _ := NewMessageItem(message)
	failure, _ := NewFailure(FailureParams{Code: FailureCode("internal_error"), Message: "internal error", Retryable: true})
	cancelFailure, _ := NewFailure(FailureParams{Code: FailureCode("cancelled"), Message: "cancelled"})
	started, _ := NewResponseStartedEvent(eventID, responseID, 0, "model-a")
	itemStarted, _ := NewItemStartedEvent(eventID, responseID, 1, ItemStartParams{Index: 0, ID: itemID, Type: ItemType("message"), Role: Role("assistant")})
	contentStarted, _ := NewContentStartedEvent(eventID, responseID, 2, itemID, 0, ContentType("text"))
	textDelta, _ := NewTextDeltaEvent(eventID, responseID, 3, itemID, 0, "hel")
	jsonDelta, _ := NewJSONDeltaEvent(eventID, responseID, 4, itemID, 0, `{"a":`)
	argumentsDelta, _ := NewToolArgumentsDeltaEvent(eventID, responseID, 5, itemID, `{"q":`)
	contentCompleted, _ := NewContentCompletedEvent(eventID, responseID, 6, itemID, 0, block)
	itemCompleted, _ := NewItemCompletedEvent(eventID, responseID, 7, 0, item)
	usageUpdated, _ := NewUsageUpdatedEvent(eventID, responseID, 8, usage)
	completed, _ := NewResponseCompletedEvent(eventID, responseID, 9, FinishReason("stop"), usage, []ItemID{itemID})
	failed, _ := NewResponseFailedEvent(eventID, responseID, 10, failure, &usage)
	cancelled, _ := NewResponseCancelledEvent(eventID, responseID, 11, cancelFailure, nil)
	return []Event{started, itemStarted, contentStarted, textDelta, jsonDelta, argumentsDelta, contentCompleted, itemCompleted, usageUpdated, completed, failed, cancelled}
}

func TestEventCodecRoundTripAllVariants(t *testing.T) {
	for _, event := range codecEvents(t) {
		encoded, err := EncodeEvent(event)
		if err != nil {
			t.Fatalf("EncodeEvent(%q) failed: %v", event.Type(), err)
		}
		decoded, err := DecodeEvent(encoded)
		if err != nil || !decoded.Valid() || decoded.Type() != event.Type() || decoded.Sequence() != event.Sequence() {
			t.Fatalf("DecodeEvent(%q) = %#v, %v", event.Type(), decoded, err)
		}
		reencoded, _ := EncodeEvent(decoded)
		if !bytes.Equal(encoded, reencoded) {
			t.Errorf("event %q round trip changed bytes:\n%s\n%s", event.Type(), encoded, reencoded)
		}
	}
}

func TestDecodeEventUsesCanonicalPreparedSize(t *testing.T) {
	event, err := NewResponseStartedEvent(testEventID(t, 1), testResponseID(t, 1), 0, "model")
	if err != nil {
		t.Fatalf("NewResponseStartedEvent() failed: %v", err)
	}
	encoded, err := EncodeEvent(event)
	if err != nil {
		t.Fatalf("EncodeEvent() failed: %v", err)
	}
	padding := MaxOrdinaryEventBytes - len(encoded) + 1
	padded := append(append([]byte(nil), encoded...), bytes.Repeat([]byte{' '}, padding)...)
	if _, err := DecodeEvent(padded); err != nil {
		t.Fatalf("DecodeEvent() rejected canonical-size event with whitespace padding: %v", err)
	}
}

func TestEventCodecStrictRejections(t *testing.T) {
	valid, _ := EncodeEvent(codecEvents(t)[0])
	tests := []struct {
		data []byte
		code ErrorCode
	}{
		{[]byte(`{"version":"1","version":"1"}`), CodeDuplicateKey},
		{insertBeforeFinalBrace(valid, `,"extra":true`), CodeUnknownField},
		{bytes.Replace(valid, []byte(`"version":"1"`), []byte(`"version":"2"`), 1), CodeInvalidVersion},
		{bytes.Replace(valid, []byte(`"type":"response_started"`), []byte(`"type":"unknown"`), 1), CodeInvalidVariant},
		{bytes.Replace(valid, []byte(`"model":"model-a"`), []byte(`"model":"model-a","extra":true`), 1), CodeUnknownField},
		{append(append([]byte(nil), valid...), []byte(` {}`)...), CodeInvalidJSON},
	}
	for _, test := range tests {
		if _, err := DecodeEvent(test.data); codeOfCanonical(err) != test.code {
			t.Errorf("code = %q, want %q; error: %v", codeOfCanonical(err), test.code, err)
		}
	}
}
