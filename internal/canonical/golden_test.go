package canonical

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGoldenFixtures(t *testing.T) {
	requests := map[string]Request{
		"request_text.json":              goldenTextRequest(t),
		"request_image_url.json":         goldenImageURLRequest(t),
		"request_image_inline.json":      goldenImageInlineRequest(t),
		"request_tools.json":             goldenToolsRequest(t),
		"request_structured_output.json": goldenStructuredRequest(t),
		"request_continuation.json":      goldenContinuationRequest(t),
	}
	for name, request := range requests {
		encoded, err := EncodeRequest(request)
		if err != nil {
			t.Fatalf("EncodeRequest(%s) failed: %v", name, err)
		}
		assertGolden(t, name, encoded)
		decoded, err := DecodeRequest(encoded)
		if err != nil {
			t.Fatalf("DecodeRequest(%s) failed: %v", name, err)
		}
		roundTrip, _ := EncodeRequest(decoded)
		if !bytes.Equal(encoded, roundTrip) {
			t.Errorf("request fixture %s did not round trip", name)
		}
	}

	responseBytes, _ := EncodeResponse(codecResponse(t))
	assertGolden(t, "response_complete.json", responseBytes)
	decodedResponse, err := DecodeResponse(responseBytes)
	if err != nil {
		t.Fatalf("DecodeResponse() failed: %v", err)
	}
	roundTripResponse, _ := EncodeResponse(decodedResponse)
	if !bytes.Equal(responseBytes, roundTripResponse) {
		t.Error("response fixture did not round trip")
	}

	eventBytes := encodeEventStream(t, codecEvents(t))
	assertGolden(t, "event_stream_complete.json", eventBytes)
	var rawEvents []json.RawMessage
	if err := json.Unmarshal(eventBytes, &rawEvents); err != nil {
		t.Fatalf("event fixture JSON failed: %v", err)
	}
	for index, raw := range rawEvents {
		event, err := DecodeEvent(raw)
		if err != nil {
			t.Fatalf("DecodeEvent(%d) failed: %v", index, err)
		}
		encoded, _ := EncodeEvent(event)
		if !bytes.Equal(raw, encoded) {
			t.Errorf("event fixture %d did not round trip", index)
		}
	}
}

func assertGolden(t *testing.T, name string, encoded []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("os.MkdirAll() failed: %v", err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatalf("os.WriteFile(%s) failed: %v", name, err)
		}
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) failed: %v", name, err)
	}
	if !bytes.Equal(golden, encoded) {
		t.Errorf("fixture %s differs; run UPDATE_GOLDEN=1", name)
	}
}

func encodeEventStream(t *testing.T, events []Event) []byte {
	t.Helper()
	encoded := make([]json.RawMessage, 0, len(events))
	for _, event := range events {
		data, err := EncodeEvent(event)
		if err != nil {
			t.Fatalf("EncodeEvent(%q) failed: %v", event.Type(), err)
		}
		encoded = append(encoded, data)
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("json.Marshal(events) failed: %v", err)
	}
	return data
}

func goldenTextRequest(t *testing.T) Request {
	message, _ := NewMessage(testItemID(t, 10), Role("user"), []ContentBlock{testTextBlock(t, "hello")})
	item, _ := NewMessageItem(message)
	request, _ := NewRequest(RequestParams{ID: testRequestID(t, 10), Model: "model-a", Input: []Item{item}})
	return request
}

func goldenImageURLRequest(t *testing.T) Request {
	image, _ := NewRemoteImage("https://example.com/image.png", "image/png", ImageDetailHigh, 640, 480)
	block, _ := NewImageBlock(image)
	message, _ := NewMessage(testItemID(t, 11), Role("user"), []ContentBlock{block})
	item, _ := NewMessageItem(message)
	request, _ := NewRequest(RequestParams{ID: testRequestID(t, 11), Model: "model-a", Input: []Item{item}})
	return request
}

func goldenImageInlineRequest(t *testing.T) Request {
	image, _ := NewInlineImage([]byte{1, 2, 3, 4}, "image/jpeg", ImageDetailLow, 0, 0)
	block, _ := NewImageBlock(image)
	message, _ := NewMessage(testItemID(t, 12), Role("user"), []ContentBlock{block})
	item, _ := NewMessageItem(message)
	request, _ := NewRequest(RequestParams{ID: testRequestID(t, 12), Model: "model-a", Input: []Item{item}})
	return request
}

func goldenToolsRequest(t *testing.T) Request {
	tool := mustTool(t, "Search")
	call, _ := NewToolCall(testItemID(t, 13), testCallID(t, 13), "Search", testJSONObject(t, `{"query":"hello"}`))
	callItem, _ := NewToolCallItem(call)
	result, _ := NewToolResult(testItemID(t, 14), testCallID(t, 13), []ContentBlock{testTextBlock(t, "result")}, false)
	resultItem, _ := NewToolResultItem(result)
	request, _ := NewRequest(RequestParams{ID: testRequestID(t, 13), Model: "model-a", Input: []Item{callItem, resultItem}, Tools: []ToolDefinition{tool}})
	return request
}

func goldenStructuredRequest(t *testing.T) Request {
	message, _ := NewMessage(testItemID(t, 15), Role("user"), []ContentBlock{testTextBlock(t, "answer")})
	item, _ := NewMessageItem(message)
	output, _ := NewOutputConstraints(OutputParams{Format: OutputFormat("json_schema"), SchemaName: "Answer", Schema: testJSONObject(t, `{"type":"object"}`)})
	request, _ := NewRequest(RequestParams{ID: testRequestID(t, 15), Model: "model-a", Input: []Item{item}, Output: output})
	return request
}

func goldenContinuationRequest(t *testing.T) Request {
	base := goldenTextRequest(t)
	continuation, _ := NewContinuation("openai", base.Model(), []byte{1, 2, 3})
	request, _ := NewRequest(RequestParams{ID: base.ID(), Model: base.Model(), Input: base.Input(), Tools: base.Tools(), ToolChoice: base.ToolChoice(), Output: base.Output(), Sampling: base.Sampling(), Continuation: &continuation})
	return request
}
