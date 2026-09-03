package canonical

import (
	"bytes"
	"strings"
	"testing"
)

func responseWithContinuation(t *testing.T, value byte) Response {
	t.Helper()
	message, err := NewMessage(testItemID(t, 1), Role("assistant"), []ContentBlock{testTextBlock(t, "hello")})
	if err != nil {
		t.Fatalf("NewMessage() failed: %v", err)
	}
	item, err := NewMessageItem(message)
	if err != nil {
		t.Fatalf("NewMessageItem() failed: %v", err)
	}
	continuation, err := NewContinuation("openai-primary", "gpt-5.2", []byte{value, 2, 3})
	if err != nil {
		t.Fatalf("NewContinuation() failed: %v", err)
	}
	response, err := NewResponse(ResponseParams{
		ID: testResponseID(t, 1), Model: "openai/reasoning", Items: []Item{item},
		FinishReason: FinishReason("stop"), Usage: Usage{}, Continuation: &continuation,
	})
	if err != nil {
		t.Fatalf("NewResponse() failed: %v", err)
	}
	return response
}

func TestResponseContinuationIsImmutable(t *testing.T) {
	response := responseWithContinuation(t, 1)
	continuation, ok := response.Continuation()
	if !ok || !continuation.Valid() {
		t.Fatalf("Continuation() = %#v, %v", continuation, ok)
	}
	data := continuation.Bytes()
	data[0] = 99
	again, _ := response.Continuation()
	if bytes.Equal(data, again.Bytes()) {
		t.Fatal("response continuation bytes were mutated")
	}
}

func TestResponsePortableEncodingExcludesContinuation(t *testing.T) {
	first := responseWithContinuation(t, 1)
	second := responseWithContinuation(t, 9)
	portableFirst, err := EncodePortableResponse(first)
	if err != nil {
		t.Fatalf("EncodePortableResponse(first) failed: %v", err)
	}
	portableSecond, err := EncodePortableResponse(second)
	if err != nil {
		t.Fatalf("EncodePortableResponse(second) failed: %v", err)
	}
	if !bytes.Equal(portableFirst, portableSecond) {
		t.Fatal("portable response encoding changed with continuation")
	}
	full, err := EncodeResponse(first)
	if err != nil {
		t.Fatalf("EncodeResponse() failed: %v", err)
	}
	if !strings.Contains(string(full), `"continuation"`) || strings.Contains(string(portableFirst), `"continuation"`) {
		t.Fatalf("continuation inclusion mismatch: full=%s portable=%s", full, portableFirst)
	}
	fullDigest, err := FullResponseDigest(first)
	if err != nil {
		t.Fatalf("FullResponseDigest() failed: %v", err)
	}
	portableDigest, err := PortableResponseDigest(first)
	if err != nil {
		t.Fatalf("PortableResponseDigest() failed: %v", err)
	}
	if bytes.Equal(fullDigest[:], portableDigest[:]) {
		t.Fatal("full response digest omitted continuation")
	}
	decoded, err := DecodeResponse(full)
	if err != nil {
		t.Fatalf("DecodeResponse() failed: %v", err)
	}
	decodedContinuation, ok := decoded.Continuation()
	if !ok || !bytes.Equal(decodedContinuation.Bytes(), []byte{1, 2, 3}) {
		t.Fatalf("decoded continuation = %#v, %v", decodedContinuation, ok)
	}
}

func TestContinuationUpdatedEventRoundTrip(t *testing.T) {
	continuation, err := NewContinuation("openai-primary", "gpt-5.2", []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("NewContinuation() failed: %v", err)
	}
	event, err := NewContinuationUpdatedEvent(testEventID(t, 1), testResponseID(t, 1), 1, continuation)
	if err != nil {
		t.Fatalf("NewContinuationUpdatedEvent() failed: %v", err)
	}
	payload, ok := event.ContinuationUpdated()
	if !ok || !payload.Valid() {
		t.Fatalf("ContinuationUpdated() = %#v, %v", payload, ok)
	}
	copyValue := payload.Continuation()
	data := copyValue.Bytes()
	data[0] = 99
	if bytes.Equal(data, payload.Continuation().Bytes()) {
		t.Fatal("event continuation bytes were mutated")
	}
	encoded, err := EncodeEvent(event)
	if err != nil {
		t.Fatalf("EncodeEvent() failed: %v", err)
	}
	decoded, err := DecodeEvent(encoded)
	if err != nil || decoded.Type() != EventType("continuation_updated") {
		t.Fatalf("DecodeEvent() = %#v, %v", decoded, err)
	}
	withUnknown := bytes.Replace(encoded, []byte(`"provider":"openai-primary"`), []byte(`"provider":"openai-primary","extra":true`), 1)
	if _, err := DecodeEvent(withUnknown); codeOfCanonical(err) != CodeUnknownField {
		t.Fatalf("unknown continuation field error = %v", err)
	}
}

func TestEmptyReasoningResponseRequiresContinuation(t *testing.T) {
	reasoning, err := NewReasoningSummary(testItemID(t, 4), nil)
	if err != nil {
		t.Fatalf("NewReasoningSummary() failed: %v", err)
	}
	item, err := NewReasoningSummaryItem(reasoning)
	if err != nil {
		t.Fatalf("NewReasoningSummaryItem() failed: %v", err)
	}
	params := ResponseParams{ID: testResponseID(t, 4), Model: "openai/reasoning", Items: []Item{item}, FinishReason: FinishReason("stop"), Usage: Usage{}}
	if _, err := NewResponse(params); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Fatalf("response without continuation error = %v", err)
	}
	continuation, err := NewContinuation("openai-primary", "gpt-5.2", []byte{4})
	if err != nil {
		t.Fatalf("NewContinuation() failed: %v", err)
	}
	params.Continuation = &continuation
	if response, err := NewResponse(params); err != nil || !response.Valid() {
		t.Fatalf("response with continuation = %#v, %v", response, err)
	}
}

func TestEmptyReasoningRequiresRequestContinuation(t *testing.T) {
	reasoning, err := NewReasoningSummary(testItemID(t, 1), nil)
	if err != nil {
		t.Fatalf("NewReasoningSummary(empty) failed: %v", err)
	}
	item, err := NewReasoningSummaryItem(reasoning)
	if err != nil {
		t.Fatalf("NewReasoningSummaryItem() failed: %v", err)
	}
	params := RequestParams{ID: testRequestID(t, 1), Model: "model-a", Input: []Item{item}}
	if _, err := NewRequest(params); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Fatalf("request without continuation error = %v", err)
	}
	continuation, err := NewContinuation("openai-primary", params.Model, []byte{1})
	if err != nil {
		t.Fatalf("NewContinuation() failed: %v", err)
	}
	params.Continuation = &continuation
	if request, err := NewRequest(params); err != nil || !request.Valid() {
		t.Fatalf("request with anchored continuation = %#v, %v", request, err)
	}
}
