package canonical

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
)

func codecRequest(t *testing.T, continuationByte byte) Request {
	t.Helper()
	textBlock := testTextBlock(t, "hello")
	image, _ := NewRemoteImage("https://example.com/image.png?sig=abc", "image/png", ImageDetailHigh, 640, 480)
	imageBlock, _ := NewImageBlock(image)
	jsonContent, _ := NewJSONContent(testJSONObject(t, `{"input":true}`))
	jsonBlock, _ := NewJSONBlock(jsonContent)
	message, _ := NewMessage(testItemID(t, 1), Role("user"), []ContentBlock{textBlock, imageBlock, jsonBlock})
	messageItem, _ := NewMessageItem(message)
	tool := mustTool(t, "Search")
	call, _ := NewToolCall(testItemID(t, 2), testCallID(t, 2), "Search", testJSONObject(t, `{"query":"hello"}`))
	callItem, _ := NewToolCallItem(call)
	result, _ := NewToolResult(testItemID(t, 3), testCallID(t, 2), []ContentBlock{testTextBlock(t, "result")}, false)
	resultItem, _ := NewToolResultItem(result)
	choice, _ := NewToolChoice(ToolChoiceKind("specific"), "Search")
	output, _ := NewOutputConstraints(OutputParams{Format: OutputFormat("json_schema"), SchemaName: "Answer", Schema: testJSONObject(t, `{"type":"object"}`), MaxTokens: 200, ParallelToolCalls: true})
	sampling, _ := NewSampling(SamplingParams{Temperature: pointer(0.5), TopP: pointer(0.9), Seed: pointer[int64](42)})
	continuation, _ := NewContinuation("openai", "model-a", []byte{continuationByte, 2, 3})
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 9), Model: "model-a", Input: []Item{messageItem, callItem, resultItem}, Tools: []ToolDefinition{tool}, ToolChoice: choice, Output: output, Sampling: sampling, Continuation: &continuation})
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}
	return request
}

func TestRequestCodecRoundTrip(t *testing.T) {
	request := codecRequest(t, 1)
	encoded, err := EncodeRequest(request)
	if err != nil {
		t.Fatalf("EncodeRequest() failed: %v", err)
	}
	if len(encoded) == 0 || encoded[len(encoded)-1] == '\n' {
		t.Errorf("encoded request has invalid framing: %q", encoded)
	}
	decoded, err := DecodeRequest(encoded)
	if err != nil || !decoded.Valid() {
		t.Fatalf("DecodeRequest() = %#v, %v", decoded, err)
	}
	reencoded, err := EncodeRequest(decoded)
	if err != nil || !bytes.Equal(encoded, reencoded) {
		t.Errorf("round trip changed bytes:\n%s\n%s\n%v", encoded, reencoded, err)
	}
	if decoded.Continuation() == nil || !bytes.Equal(decoded.Continuation().Bytes(), []byte{1, 2, 3}) {
		t.Error("continuation did not round trip")
	}
}

func TestRequestPortableEncodingAndDigests(t *testing.T) {
	first := codecRequest(t, 1)
	second := codecRequest(t, 9)
	firstPortable, err := EncodePortableRequest(first)
	if err != nil {
		t.Fatalf("EncodePortableRequest() failed: %v", err)
	}
	secondPortable, _ := EncodePortableRequest(second)
	if !bytes.Equal(firstPortable, secondPortable) || bytes.Contains(firstPortable, []byte("openai")) {
		t.Error("continuation changed portable encoding")
	}
	portableDigest, _ := PortableDigest(first)
	if portableDigest != sha256.Sum256(firstPortable) {
		t.Error("PortableDigest() does not hash portable encoding")
	}
	firstFull, _ := FullDigest(first)
	secondFull, _ := FullDigest(second)
	if firstFull == secondFull || firstFull == portableDigest {
		t.Error("full digest ignored continuation")
	}
	decoded, err := DecodeRequest(firstPortable)
	if err != nil || decoded.Continuation() != nil {
		t.Errorf("portable decode = %#v, %v", decoded, err)
	}
}

func TestRequestCodecStrictRejections(t *testing.T) {
	valid, _ := EncodeRequest(codecRequest(t, 1))
	tests := []struct {
		name string
		data []byte
		code ErrorCode
	}{
		{"duplicate", []byte(`{"version":"1","version":"1"}`), CodeDuplicateKey},
		{"unknown top", insertBeforeFinalBrace(valid, `,"extra":true`), CodeUnknownField},
		{"version", bytes.Replace(valid, []byte(`"version":"1"`), []byte(`"version":"2"`), 1), CodeInvalidVersion},
		{"item discriminator", bytes.Replace(valid, []byte(`"type":"message"`), []byte(`"type":"unknown"`), 1), CodeInvalidVariant},
		{"unknown item", bytes.Replace(valid, []byte(`"role":"user"`), []byte(`"role":"user","extra":true`), 1), CodeUnknownField},
		{"bad continuation base64", bytes.Replace(valid, []byte(`"data":"AQID"`), []byte(`"data":"***"`), 1), CodeInvalidInvariant},
		{"trailing", append(append([]byte(nil), valid...), []byte(` {}`)...), CodeInvalidJSON},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeRequest(test.data)
			if codeOfCanonical(err) != test.code {
				t.Errorf("code = %q, want %q; error: %v", codeOfCanonical(err), test.code, err)
			}
		})
	}
}

func insertBeforeFinalBrace(data []byte, addition string) []byte {
	index := strings.LastIndexByte(string(data), '}')
	if index < 0 {
		return data
	}
	result := append([]byte(nil), data[:index]...)
	result = append(result, addition...)
	return append(result, data[index:]...)
}
