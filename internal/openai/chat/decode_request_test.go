package chat

import (
	"errors"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func decodeTestIDs() *countingIDs { return &countingIDs{} }

func decodeTestRequestID(t *testing.T) canonical.RequestID {
	t.Helper()
	ids := &countingIDs{}
	id, err := ids.RequestID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDecodeRequestSimpleUserMessage(t *testing.T) {
	decoded, err := DecodeRequest([]byte(`{
		"model":"deepseek-chat","stream":true,
		"messages":[{"role":"user","content":"inspect the payload"}]
	}`), decodeTestRequestID(t), decodeTestIDs())
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Stream {
		t.Fatal("stream flag lost")
	}
	if decoded.Request.Model() != "deepseek-chat" {
		t.Fatalf("model = %q", decoded.Request.Model())
	}
	if len(decoded.Request.Input()) != 1 {
		t.Fatalf("input = %d items", len(decoded.Request.Input()))
	}
	message, ok := decoded.Request.Input()[0].Message()
	if !ok || message.Role() != canonical.Role("user") || len(message.Content()) != 1 {
		t.Fatalf("message = %#v", decoded.Request.Input()[0])
	}
	if text, ok := message.Content()[0].Text(); !ok || text.Text() != "inspect the payload" {
		t.Fatalf("content = %#v", message.Content())
	}
}

func TestDecodeRequestSystemAndPartsAndImages(t *testing.T) {
	decoded, err := DecodeRequest([]byte(`{
		"model":"vision-model",
		"messages":[
			{"role":"system","content":"be precise"},
			{"role":"user","content":[
				{"type":"text","text":"describe"},
				{"type":"image_url","image_url":{"url":"https://example.com/pic.png","detail":"high"}}
			]}
		]
	}`), decodeTestRequestID(t), decodeTestIDs())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Request.Input()) != 2 {
		t.Fatalf("input = %d items", len(decoded.Request.Input()))
	}
	system, ok := decoded.Request.Input()[0].Message()
	if !ok || system.Role() != canonical.Role("system") {
		t.Fatalf("item[0] = %#v", decoded.Request.Input()[0])
	}
	user, ok := decoded.Request.Input()[1].Message()
	if !ok || len(user.Content()) != 2 {
		t.Fatalf("item[1] = %#v", decoded.Request.Input()[1])
	}
	if _, isImage := user.Content()[1].Image(); !isImage {
		t.Fatalf("block[1] = %#v, want image", user.Content()[1])
	}
}

func TestDecodeRequestInlineDataURL(t *testing.T) {
	// 1x1 PNG
	const dataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	decoded, err := DecodeRequest([]byte(`{
		"model":"vision-model",
		"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"`+dataURL+`"}}]}]
	}`), decodeTestRequestID(t), decodeTestIDs())
	if err != nil {
		t.Fatal(err)
	}
	user, ok := decoded.Request.Input()[0].Message()
	if !ok {
		t.Fatal("item[0] is not a message")
	}
	image, isImage := user.Content()[0].Image()
	if !isImage {
		t.Fatalf("block[0] = %#v, want image", user.Content()[0])
	}
	if image.MIME() != "image/png" {
		t.Fatalf("mime = %q", image.MIME())
	}
}

func TestDecodeRequestToolCallAndResultLinkage(t *testing.T) {
	decoded, err := DecodeRequest([]byte(`{
		"model":"deepseek-chat",
		"messages":[
			{"role":"user","content":"look it up"},
			{"role":"assistant","tool_calls":[{"id":"call_wire_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},
			{"role":"tool","tool_call_id":"call_wire_1","content":"result text"}
		],
		"tools":[{"type":"function","function":{"name":"lookup","description":"finds","parameters":{"type":"object"}}}]
	}`), decodeTestRequestID(t), decodeTestIDs())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Request.Input()) != 3 {
		t.Fatalf("input = %d items", len(decoded.Request.Input()))
	}
	call, ok := decoded.Request.Input()[1].ToolCall()
	if !ok {
		t.Fatalf("item[1] = %#v, want tool_call", decoded.Request.Input()[1])
	}
	result, ok := decoded.Request.Input()[2].ToolResult()
	if !ok {
		t.Fatalf("item[2] = %#v, want tool_result", decoded.Request.Input()[2])
	}
	if result.CallID() != call.CallID() {
		t.Fatalf("result call id %q != call id %q", result.CallID(), call.CallID())
	}
}

func TestDecodeRequestToolsAndControls(t *testing.T) {
	decoded, err := DecodeRequest([]byte(`{
		"model":"deepseek-chat",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"type":"function","function":{"name":"lookup","description":"finds","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"lookup"}},
		"temperature":0.4,"max_tokens":256,
		"response_format":{"type":"json_schema","json_schema":{"name":"out","schema":{"type":"object"},"strict":true}},
		"parallel_tool_calls":false
	}`), decodeTestRequestID(t), decodeTestIDs())
	if err != nil {
		t.Fatal(err)
	}
	tools := decoded.Request.Tools()
	if len(tools) != 1 || tools[0].Name() != "lookup" {
		t.Fatalf("tools = %#v", tools)
	}
	if decoded.Request.ToolChoice().Kind() != canonical.ToolChoiceKind("specific") || decoded.Request.ToolChoice().Name() != "lookup" {
		t.Fatalf("tool_choice = %#v", decoded.Request.ToolChoice())
	}
	if decoded.Request.Output().Format() != canonical.OutputFormat("json_schema") || decoded.Request.Output().Enforcement() != canonical.SchemaEnforcementStrict {
		t.Fatalf("output = %#v", decoded.Request.Output())
	}
	if decoded.Request.Output().MaxTokens() != 256 {
		t.Fatalf("max tokens = %d", decoded.Request.Output().MaxTokens())
	}
	if decoded.Request.Output().ParallelToolCalls() {
		t.Fatal("parallel tool calls should be false")
	}
	if temp, ok := decoded.Request.Sampling().Temperature(); !ok || temp != 0.4 {
		t.Fatalf("temperature = %#v", decoded.Request.Sampling())
	}
}

func TestDecodeRequestRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantPath string
	}{
		{"empty model", `{"model":"","messages":[{"role":"user","content":"x"}]}`, "request.model"},
		{"no messages", `{"model":"m","messages":[]}`, "request.messages"},
		{"reasoning content", `{"model":"m","messages":[{"role":"user","content":"x","reasoning_content":"y"}]}`, "request.messages[0].reasoning_content"},
		{"unknown tool call", `{"model":"m","messages":[{"role":"user","content":"x"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"nope","content":"x"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, "request.messages[2].tool_call_id"},
		{"undeclared history tool", `{"model":"m","messages":[{"role":"user","content":"x"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]}`, "request.input"},
		{"http image", `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://example.com/x.png"}}]}]}`, "request.messages[0].content[0].image_url.url"},
		{"unknown part type", `{"model":"m","messages":[{"role":"user","content":[{"type":"audio"}]}]}`, "request.messages[0].content[0].type"},
		{"bad tool choice", `{"model":"m","messages":[{"role":"user","content":"x"}],"tool_choice":{"type":"weird"}}`, "request.tool_choice.type"},
		{"tool choice missing name", `{"model":"m","messages":[{"role":"user","content":"x"}],"tool_choice":{"type":"function"}}`, "request.tool_choice.function.name"},
		{"bad json schema", `{"model":"m","messages":[{"role":"user","content":"x"}],"response_format":{"type":"json_schema","json_schema":{"name":"n","schema":123}}}`, "request.output.schema"},
		{"zero max tokens", `{"model":"m","messages":[{"role":"user","content":"x"}],"max_tokens":0}`, "request.max_tokens"},
		{"malformed json", `{"model":`, "request.json"},
		{"duplicate wire call id", `{"model":"m","messages":[{"role":"user","content":"x"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, "request.messages[2].tool_calls[0].id"},
		{"empty tool result", `{"model":"m","messages":[{"role":"user","content":"x"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":""}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, "request.messages[2].content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeRequest([]byte(tc.body), decodeTestRequestID(t), decodeTestIDs())
			if err == nil {
				t.Fatalf("accepted invalid request")
			}
			var chatErr *Error
			if !errors.As(err, &chatErr) {
				t.Fatalf("error = %v is not a chat error", err)
			}
			if chatErr.Path != tc.wantPath {
				t.Fatalf("error path = %q, want %q (error: %v)", chatErr.Path, tc.wantPath, err)
			}
			if chatErr.Code == "protocol" {
				t.Fatalf("client fault classified as protocol: %v", err)
			}
		})
	}
}

func TestDecodeRequestToleratesUnknownFields(t *testing.T) {
	decoded, err := DecodeRequest([]byte(`{
		"model":"m","messages":[{"role":"user","content":"x"}],
		"vendor_extension":{"nested":true},"logprobs":true,"frequency_penalty":0.5
	}`), decodeTestRequestID(t), decodeTestIDs())
	if err != nil {
		t.Fatalf("unknown vendor fields rejected: %v", err)
	}
	if decoded.Request.Model() != "m" {
		t.Fatalf("model = %q", decoded.Request.Model())
	}
}

func TestDecodeRequestStringToolChoice(t *testing.T) {
	for _, form := range []string{`"auto"`, `"none"`} {
		decoded, err := DecodeRequest([]byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"tool_choice":`+form+`}`), decodeTestRequestID(t), decodeTestIDs())
		if err != nil {
			t.Fatalf("tool_choice %s rejected: %v", form, err)
		}
		want := canonical.ToolChoiceKind(strings.Trim(form, `"`))
		if decoded.Request.ToolChoice().Kind() != want {
			t.Fatalf("tool_choice kind = %q, want %q", decoded.Request.ToolChoice().Kind(), want)
		}
	}
	if _, err := DecodeRequest([]byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"tool_choice":"weird"}`), decodeTestRequestID(t), decodeTestIDs()); err == nil {
		t.Fatal("string tool_choice weird accepted")
	}
}

func TestDecodeRequestPreservesAssistantTextBeforeToolCalls(t *testing.T) {
	decoded, err := DecodeRequest([]byte(`{
		"model":"m",
		"messages":[
			{"role":"user","content":"x"},
			{"role":"assistant","content":"let me look","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"c1","content":"done"}
		],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]
	}`), decodeTestRequestID(t), decodeTestIDs())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Request.Input()) != 4 {
		t.Fatalf("input = %d items, want 4 (user, assistant text, tool_call, tool_result)", len(decoded.Request.Input()))
	}
	message, ok := decoded.Request.Input()[1].Message()
	if !ok || message.Role() != canonical.Role("assistant") {
		t.Fatalf("item[1] = %#v, want assistant message", decoded.Request.Input()[1])
	}
}

func TestDecodeRequestRequiredToolChoiceNeedsTools(t *testing.T) {
	decoded, err := DecodeRequest([]byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"tool_choice":"required","tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`), decodeTestRequestID(t), decodeTestIDs())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.ToolChoice().Kind() != canonical.ToolChoiceKind("required") {
		t.Fatalf("tool_choice = %q", decoded.Request.ToolChoice().Kind())
	}
}
