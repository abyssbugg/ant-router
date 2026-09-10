package canonical

import (
	"bytes"
	"testing"
)

func codecResponse(t *testing.T) Response {
	t.Helper()
	message, _ := NewMessage(testItemID(t, 1), Role("assistant"), []ContentBlock{testTextBlock(t, "hello")})
	messageItem, _ := NewMessageItem(message)
	text, _ := NewTextContent("summary")
	reasoning, _ := NewReasoningSummary(testItemID(t, 2), []TextContent{text})
	reasoningItem, _ := NewReasoningSummaryItem(reasoning)
	call, _ := NewToolCall(testItemID(t, 3), testCallID(t, 3), "Search", testJSONObject(t, `{"query":"hello"}`))
	callItem, _ := NewToolCallItem(call)
	usage, _ := NewUsage(UsageParams{InputTokens: 10, OutputTokens: 5, ReasoningTokens: 2, CacheReadTokens: 3, TotalTokens: 15})
	response, err := NewResponse(ResponseParams{ID: testResponseID(t, 1), Model: "model-a", Items: []Item{messageItem, reasoningItem, callItem}, FinishReason: FinishReason("tool_call"), Usage: usage})
	if err != nil {
		t.Fatalf("NewResponse() failed: %v", err)
	}
	return response
}

func TestResponseCodecRoundTrip(t *testing.T) {
	encoded, err := EncodeResponse(codecResponse(t))
	if err != nil {
		t.Fatalf("EncodeResponse() failed: %v", err)
	}
	decoded, err := DecodeResponse(encoded)
	if err != nil || !decoded.Valid() {
		t.Fatalf("DecodeResponse() = %#v, %v", decoded, err)
	}
	reencoded, _ := EncodeResponse(decoded)
	if !bytes.Equal(encoded, reencoded) {
		t.Errorf("round trip changed bytes:\n%s\n%s", encoded, reencoded)
	}
}

func TestResponseCodecStrictRejections(t *testing.T) {
	valid, _ := EncodeResponse(codecResponse(t))
	tests := []struct {
		data []byte
		code ErrorCode
	}{
		{[]byte(`{"version":"1","version":"1"}`), CodeDuplicateKey},
		{insertBeforeFinalBrace(valid, `,"extra":true`), CodeUnknownField},
		{bytes.Replace(valid, []byte(`"version":"2"`), []byte(`"version":"3"`), 1), CodeInvalidVersion},
		{bytes.Replace(valid, []byte(`"type":"message"`), []byte(`"type":"unknown"`), 1), CodeInvalidVariant},
		{append(append([]byte(nil), valid...), []byte(` {}`)...), CodeInvalidJSON},
	}
	for _, test := range tests {
		if _, err := DecodeResponse(test.data); codeOfCanonical(err) != test.code {
			t.Errorf("code = %q, want %q; error: %v", codeOfCanonical(err), test.code, err)
		}
	}
}
