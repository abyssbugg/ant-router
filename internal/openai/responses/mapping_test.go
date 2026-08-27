package responses

import (
	"bytes"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func TestDecodeMapsEasyInputMessage(t *testing.T) {
	parsed, err := Decode(bytes.NewBufferString(`{"model":"mock/text","input":[{"role":"user","content":"hello"}]}`), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	message, ok := parsed.Request.Input()[0].Message()
	text, textOK := message.Content()[0].Text()
	if !ok || !textOK || message.Role() != canonical.Role("user") || text.Text() != "hello" {
		t.Fatalf("easy message = %#v", parsed.Request.Input()[0])
	}
}

func TestDecodeMapsToolsStructuredOutputAndReasoning(t *testing.T) {
	body := `{
		"model":"mock/tool",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"find docs"}]}],
		"tools":[{"type":"function","name":"Search","description":"find docs","parameters":{"type":"object","properties":{"query":{"type":"string"}}},"strict":true}],
		"tool_choice":{"type":"function","name":"Search"},
		"parallel_tool_calls":true,
		"text":{"format":{"type":"json_schema","name":"Answer","schema":{"type":"object"},"strict":true}},
		"reasoning":{"summary":"concise"},
		"temperature":0.2,
		"top_p":0.9,
		"max_output_tokens":512,
		"stream":true
	}`
	parsed, err := Decode(bytes.NewBufferString(body), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Stream || parsed.Request.Model() != "mock/tool" || parsed.Request.ReasoningSummary() != canonical.ReasoningSummaryConcise {
		t.Fatalf("parsed request = %#v", parsed)
	}
	items := parsed.Request.Input()
	message, ok := items[0].Message()
	text, textOK := message.Content()[0].Text()
	if !ok || !textOK || message.Role() != canonical.Role("user") || text.Text() != "find docs" {
		t.Fatalf("message = %#v", items[0])
	}
	tools := parsed.Request.Tools()
	if len(tools) != 1 || tools[0].Name() != "Search" || tools[0].Enforcement() != canonical.SchemaEnforcementStrict {
		t.Fatalf("tools = %#v", tools)
	}
	if parsed.Request.ToolChoice().Kind() != canonical.ToolChoiceKind("specific") || parsed.Request.ToolChoice().Name() != "Search" {
		t.Fatalf("tool choice = %#v", parsed.Request.ToolChoice())
	}
	output := parsed.Request.Output()
	if output.Format() != canonical.OutputFormat("json_schema") || output.SchemaName() != "Answer" || output.Enforcement() != canonical.SchemaEnforcementStrict || !output.ParallelToolCalls() || output.MaxTokens() != 512 {
		t.Fatalf("output = %#v", output)
	}
	temperature, temperatureOK := parsed.Request.Sampling().Temperature()
	topP, topPOK := parsed.Request.Sampling().TopP()
	if !temperatureOK || !topPOK || temperature != 0.2 || topP != 0.9 {
		t.Fatalf("sampling = %#v", parsed.Request.Sampling())
	}
}

func TestDecodeMapsFunctionCallAndOutput(t *testing.T) {
	body := `{
		"model":"mock/tool",
		"input":[
			{"type":"function_call","call_id":"external-1","name":"Search","arguments":"{\"query\":\"docs\"}"},
			{"type":"function_call_output","call_id":"external-1","output":"found"}
		],
		"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]
	}`
	parsed, err := Decode(bytes.NewBufferString(body), &sequenceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	items := parsed.Request.Input()
	call, ok := items[0].ToolCall()
	if !ok || call.Name() != "Search" || string(call.Arguments().Bytes()) != `{"query":"docs"}` {
		t.Fatalf("call = %#v", items[0])
	}
	result, ok := items[1].ToolResult()
	resultText, textOK := result.Content()[0].Text()
	if !ok || !textOK || result.CallID() != call.CallID() || resultText.Text() != "found" {
		t.Fatalf("result = %#v", items[1])
	}
}

func TestDecodeRejectsRecursiveUnknownAndInvalidReferences(t *testing.T) {
	cases := []struct {
		name, body, code, path string
	}{
		{"unknown content field", `{"model":"mock/text","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"x","secret":"ultra-private-request-value"}]}]}`, "unknown_field", "input[0].content[0].secret"},
		{"unsupported image", `{"model":"mock/text","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`, "unsupported_field", "input[0].content[0].type"},
		{"forward result", `{"model":"mock/tool","input":[{"type":"function_call_output","call_id":"missing","output":"x"}]}`, "invalid_field", "input[0].call_id"},
		{"duplicate external call", `{"model":"mock/tool","input":[{"type":"function_call","call_id":"same","name":"Search","arguments":"{}"},{"type":"function_call","call_id":"same","name":"Search","arguments":"{}"}],"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]}`, "invalid_field", "input[1].call_id"},
		{"unsupported tool type", `{"model":"mock/text","input":"x","tools":[{"type":"web_search"}]}`, "unsupported_field", "tools[0].type"},
		{"undeclared call", `{"model":"mock/tool","input":[{"type":"function_call","call_id":"one","name":"Other","arguments":"{}"}],"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]}`, "invalid_field", "input[0].name"},
		{"duplicate output", `{"model":"mock/tool","input":[{"type":"function_call","call_id":"one","name":"Search","arguments":"{}"},{"type":"function_call_output","call_id":"one","output":"a"},{"type":"function_call_output","call_id":"one","output":"b"}],"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]}`, "invalid_field", "input[2].call_id"},
		{"undeclared specific choice", `{"model":"mock/tool","input":"x","tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}],"tool_choice":{"type":"function","name":"Other"}}`, "invalid_field", "tool_choice.name"},
		{"parallel without tools", `{"model":"mock/text","input":"x","parallel_tool_calls":true}`, "invalid_field", "parallel_tool_calls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(bytes.NewBufferString(tc.body), &sequenceIDs{})
			if CodeOf(err) != ErrorCode(tc.code) || PathOf(err) != tc.path {
				t.Fatalf("error = %v, code/path = %q/%q", err, CodeOf(err), PathOf(err))
			}
			if bytes.Contains([]byte(err.Error()), []byte("ultra-private-request-value")) {
				t.Fatalf("error leaked request value: %v", err)
			}
		})
	}
}
