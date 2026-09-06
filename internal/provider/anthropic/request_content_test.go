package anthropic

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestMapRequestPreservesImagesCallsResultsAndOrder(t *testing.T) {
	png := make([]byte, 24)
	copy(png, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	binary.BigEndian.PutUint32(png[8:12], 13)
	copy(png[12:16], []byte("IHDR"))
	binary.BigEndian.PutUint32(png[16:20], 12)
	binary.BigEndian.PutUint32(png[20:24], 7)
	inline := base64.StdEncoding.EncodeToString(png)
	body := fmt.Sprintf(`{
		"model":"anthropic/public",
		"input":[
			{"role":"user","content":[{"type":"input_text","text":"look"},{"type":"input_image","image_url":"data:image/png;base64,%s"},{"type":"input_image","image_url":"https://example.com/image.png"}]},
			{"type":"function_call","call_id":"external","name":"Search","arguments":"{\"query\":\"docs\"}"},
			{"type":"function_call_output","call_id":"external","output":"found"}
		],
		"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]
	}`, inline)
	params, err := mapRequest(decodeRequest(t, body), "claude-upstream", 1024, 4096)
	if err != nil {
		t.Fatalf("mapRequest: %v", err)
	}
	object := requestObject(t, params)
	messages := asArray(t, object["messages"], "messages")
	if len(messages) != 3 {
		t.Fatalf("messages length = %d, want 3", len(messages))
	}
	user := asObject(t, messages[0], "messages[0]")
	assistant := asObject(t, messages[1], "messages[1]")
	resultTurn := asObject(t, messages[2], "messages[2]")
	if user["role"] != "user" || assistant["role"] != "assistant" || resultTurn["role"] != "user" {
		t.Fatalf("roles = %#v", messages)
	}
	userContent := asArray(t, user["content"], "user.content")
	if len(userContent) != 3 {
		t.Fatalf("user content = %#v", userContent)
	}
	base64Source := asObject(t, asObject(t, userContent[1], "image")["source"], "source")
	if base64Source["type"] != "base64" || base64Source["media_type"] != "image/png" || base64Source["data"] != inline {
		t.Fatalf("base64 source = %#v", base64Source)
	}
	urlSource := asObject(t, asObject(t, userContent[2], "url image")["source"], "source")
	if urlSource["type"] != "url" || urlSource["url"] != "https://example.com/image.png" {
		t.Fatalf("URL source = %#v", urlSource)
	}
	assistantContent := asArray(t, assistant["content"], "assistant.content")
	call := asObject(t, assistantContent[0], "tool use")
	resultContent := asArray(t, resultTurn["content"], "result.content")
	result := asObject(t, resultContent[0], "tool result")
	if call["type"] != "tool_use" || result["type"] != "tool_result" || result["tool_use_id"] != call["id"] {
		t.Fatalf("call/result = %#v / %#v", call, result)
	}
}

func TestMapRequestRejectsUnsupportedSemantics(t *testing.T) {
	tests := map[string]string{
		"reasoning":          `{"model":"anthropic/public","input":"hello","reasoning":{"summary":"concise"}}`,
		"best effort schema": `{"model":"anthropic/public","input":"hello","text":{"format":{"type":"json_schema","name":"x","schema":{"type":"object"},"strict":false}}}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := mapRequest(decodeRequest(t, body), "claude-upstream", 1024, 4096)
			if err == nil || strings.Contains(err.Error(), "hello") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
