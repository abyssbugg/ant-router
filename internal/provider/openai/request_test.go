package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	inbound "github.com/abyssbugg/ant-router/internal/openai/responses"
)

type mapperIDs struct{ next byte }

func (g *mapperIDs) reader() *bytes.Reader {
	g.next++
	return bytes.NewReader(bytes.Repeat([]byte{g.next}, 16))
}

func (g *mapperIDs) RequestID() (canonical.RequestID, error) {
	return canonical.GenerateRequestID(g.reader())
}
func (g *mapperIDs) ItemID() (canonical.ItemID, error) { return canonical.GenerateItemID(g.reader()) }
func (g *mapperIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(g.reader())
}

func decodeCanonicalRequest(t *testing.T, body string) canonical.Request {
	t.Helper()
	parsed, err := inbound.Decode(bytes.NewBufferString(body), &mapperIDs{})
	if err != nil {
		t.Fatalf("decode canonical request: %v", err)
	}
	return parsed.Request
}

func marshaledObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal SDK params: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatalf("decode SDK params: %v", err)
	}
	return object
}

func objectField(t *testing.T, value any, path string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want object", path, value)
	}
	return object
}

func arrayField(t *testing.T, value any, path string) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("%s is %T, want array", path, value)
	}
	return array
}

func TestMapRequestPreservesControlsAndUsesUpstreamModel(t *testing.T) {
	request := decodeCanonicalRequest(t, `{
		"model":"public/openai",
		"input":[{"role":"user","content":"find docs"}],
		"tools":[{"type":"function","name":"Search","description":"find docs","parameters":{"type":"object","properties":{"limit":{"type":"integer","maximum":9007199254740993}}},"strict":true}],
		"tool_choice":{"type":"function","name":"Search"},
		"parallel_tool_calls":true,
		"text":{"format":{"type":"json_schema","name":"Answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}},"strict":true}},
		"reasoning":{"summary":"concise"},
		"temperature":0.2,
		"top_p":0.9,
		"max_output_tokens":512
	}`)
	params, err := mapRequest(request, "gpt-5.2")
	if err != nil {
		t.Fatalf("mapRequest: %v", err)
	}
	object := marshaledObject(t, params)
	if object["model"] != "gpt-5.2" || object["temperature"] != json.Number("0.2") || object["top_p"] != json.Number("0.9") || object["max_output_tokens"] != json.Number("512") || object["parallel_tool_calls"] != true {
		t.Fatalf("mapped controls = %#v", object)
	}
	input := arrayField(t, object["input"], "input")
	if len(input) != 1 {
		t.Fatalf("input length = %d, want 1", len(input))
	}
	message := objectField(t, input[0], "input[0]")
	if message["role"] != "user" || (message["type"] != nil && message["type"] != "message") {
		t.Fatalf("message type/role = %v/%v", message["type"], message["role"])
	}
	messageContent := arrayField(t, message["content"], "input[0].content")
	if len(messageContent) != 1 || objectField(t, messageContent[0], "input[0].content[0]")["text"] != "find docs" {
		t.Fatalf("message content = %#v", messageContent)
	}
	tools := arrayField(t, object["tools"], "tools")
	tool := objectField(t, tools[0], "tools[0]")
	if tool["type"] != "function" || tool["name"] != "Search" || tool["description"] != "find docs" || tool["strict"] != true {
		t.Fatalf("function tool = %#v", tool)
	}
	parameters := objectField(t, tool["parameters"], "tools[0].parameters")
	properties := objectField(t, parameters["properties"], "tools[0].parameters.properties")
	limit := objectField(t, properties["limit"], "tools[0].parameters.properties.limit")
	if limit["maximum"] != json.Number("9007199254740993") {
		t.Fatalf("schema maximum = %v (%T)", limit["maximum"], limit["maximum"])
	}
	choice := objectField(t, object["tool_choice"], "tool_choice")
	if choice["type"] != "function" || choice["name"] != "Search" {
		t.Fatalf("tool_choice = %#v", choice)
	}
	format := objectField(t, objectField(t, object["text"], "text")["format"], "text.format")
	if format["type"] != "json_schema" || format["name"] != "Answer" || format["strict"] != true {
		t.Fatalf("text.format = %#v", format)
	}
	if objectField(t, object["reasoning"], "reasoning")["summary"] != "concise" {
		t.Fatalf("reasoning = %#v", object["reasoning"])
	}
}

func TestMapRequestForcesStatelessControlsAndOmitsInactiveControls(t *testing.T) {
	params, err := mapRequest(decodeCanonicalRequest(t, `{"model":"public/openai","input":"hello"}`), "gpt-5.2")
	if err != nil {
		t.Fatal(err)
	}
	object := marshaledObject(t, params)
	if object["store"] != false {
		t.Fatalf("store = %#v, want false", object["store"])
	}
	include := arrayField(t, object["include"], "include")
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", include)
	}
	for _, field := range []string{"tool_choice", "tools", "parallel_tool_calls", "temperature", "top_p", "max_output_tokens", "reasoning", "text"} {
		if _, exists := object[field]; exists {
			t.Fatalf("inactive field %q was emitted: %#v", field, object[field])
		}
	}
}

func TestMapRequestPreservesImagesCallsAndResults(t *testing.T) {
	png := make([]byte, 24)
	copy(png, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	binary.BigEndian.PutUint32(png[8:12], 13)
	copy(png[12:16], []byte("IHDR"))
	binary.BigEndian.PutUint32(png[16:20], 12)
	binary.BigEndian.PutUint32(png[20:24], 7)
	inline := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	body := fmt.Sprintf(`{
		"model":"public/openai",
		"input":[
			{"role":"user","content":[{"type":"input_text","text":"look"},{"type":"input_image","image_url":%q,"detail":"high"},{"type":"input_image","image_url":"https://example.com/signed?private=value","detail":"low"}]},
			{"type":"function_call","call_id":"external","name":"Search","arguments":"{\"query\":\"docs\"}"},
			{"type":"function_call_output","call_id":"external","output":"found"}
		],
		"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]
	}`, inline)
	params, err := mapRequest(decodeCanonicalRequest(t, body), "gpt-5.2")
	if err != nil {
		t.Fatalf("mapRequest: %v", err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	input := arrayField(t, object["input"], "input")
	if len(input) != 3 {
		t.Fatalf("input length = %d, want 3", len(input))
	}
	message := objectField(t, input[0], "input[0]")
	content := arrayField(t, message["content"], "input[0].content")
	if len(content) != 3 {
		t.Fatalf("content length = %d, want 3", len(content))
	}
	inlineImage := objectField(t, content[1], "input[0].content[1]")
	remoteImage := objectField(t, content[2], "input[0].content[2]")
	if inlineImage["type"] != "input_image" || inlineImage["detail"] != "high" || !bytes.HasPrefix([]byte(fmt.Sprint(inlineImage["image_url"])), []byte("data:image/png;base64,")) {
		t.Fatalf("inline image = %#v", inlineImage)
	}
	if remoteImage["type"] != "input_image" || remoteImage["detail"] != "low" || remoteImage["image_url"] != "https://example.com/signed?private=value" {
		t.Fatalf("remote image = %#v", remoteImage)
	}
	call := objectField(t, input[1], "input[1]")
	result := objectField(t, input[2], "input[2]")
	if call["type"] != "function_call" || call["name"] != "Search" || call["arguments"] != `{"query":"docs"}` {
		t.Fatalf("function call = %#v", call)
	}
	if result["type"] != "function_call_output" || result["output"] != "found" || result["call_id"] != call["call_id"] {
		t.Fatalf("function result = %#v, call = %#v", result, call)
	}
}

type mapperAffinityResolver struct{ providerID, upstreamModel string }

func (r mapperAffinityResolver) ResolveModel(string) (string, string, error) {
	return r.providerID, r.upstreamModel, nil
}

func decodeContinuationRequest(t *testing.T) canonical.Request {
	t.Helper()
	body := `{
		"model":"openai/public",
		"store":false,
		"include":["reasoning.encrypted_content"],
		"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}],
		"input":[
			{"type":"reasoning","id":"rs_upstream","summary":[{"type":"summary_text","text":"checked docs"}],"encrypted_content":"cipher-marker","status":"completed"},
			{"type":"function_call","id":"fc_upstream","status":"completed","call_id":"call-1","name":"Search","arguments":"{}"},
			{"type":"function_call_output","call_id":"call-1","output":"found"}
		]
	}`
	parsed, err := inbound.DecodeWithAffinity(bytes.NewBufferString(body), &mapperIDs{}, mapperAffinityResolver{providerID: "openai-primary", upstreamModel: "gpt-upstream"})
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Request
}

func TestMapRequestReplaysReasoningContinuationInInputOrder(t *testing.T) {
	params, err := mapRequest(decodeContinuationRequest(t), "gpt-upstream")
	if err != nil {
		t.Fatal(err)
	}
	object := marshaledObject(t, params)
	if object["store"] != false {
		t.Fatalf("store = %#v, want false", object["store"])
	}
	include := arrayField(t, object["include"], "include")
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", include)
	}
	input := arrayField(t, object["input"], "input")
	if len(input) != 3 {
		t.Fatalf("input length = %d, want 3", len(input))
	}
	reasoning := objectField(t, input[0], "input[0]")
	if reasoning["type"] != "reasoning" || reasoning["id"] != "rs_upstream" || reasoning["encrypted_content"] != "cipher-marker" {
		t.Fatalf("reasoning replay = %#v", reasoning)
	}
	summary := arrayField(t, reasoning["summary"], "input[0].summary")
	if len(summary) != 1 || objectField(t, summary[0], "input[0].summary[0]")["text"] != "checked docs" {
		t.Fatalf("reasoning summary = %#v", summary)
	}
	if objectField(t, input[1], "input[1]")["type"] != "function_call" || objectField(t, input[2], "input[2]")["type"] != "function_call_output" {
		t.Fatalf("replay order = %#v", input)
	}
}

func TestMapRequestRejectsContinuationUpstreamMismatch(t *testing.T) {
	request := decodeContinuationRequest(t)
	if _, err := mapRequest(request, "gpt-other"); executionErrorPath(err) != "continuation" {
		t.Fatalf("error path = %q, want continuation", executionErrorPath(err))
	}
}

func rebuildRequest(t *testing.T, request canonical.Request, mutate func(*canonical.RequestParams)) canonical.Request {
	t.Helper()
	params := canonical.RequestParams{
		ID:               request.ID(),
		Model:            request.Model(),
		Input:            request.Input(),
		Tools:            request.Tools(),
		ToolChoice:       request.ToolChoice(),
		Output:           request.Output(),
		Sampling:         request.Sampling(),
		Continuation:     request.Continuation(),
		ReasoningSummary: request.ReasoningSummary(),
	}
	mutate(&params)
	rebuilt, err := canonical.NewRequest(params)
	if err != nil {
		t.Fatalf("rebuild request: %v", err)
	}
	return rebuilt
}

func executionErrorPath(err error) string {
	var target *execution.Error
	if errors.As(err, &target) && target != nil {
		return target.Path
	}
	return ""
}

func TestMapRequestRejectsUnrepresentableInputItems(t *testing.T) {
	toolRequest := decodeCanonicalRequest(t, `{
		"model":"public/openai",
		"input":[{"type":"function_call","call_id":"one","name":"Search","arguments":"{}"},{"type":"function_call_output","call_id":"one","output":"failed"}],
		"tools":[{"type":"function","name":"Search","parameters":{"type":"object"}}]
	}`)
	toolItems := toolRequest.Input()
	result, _ := toolItems[1].ToolResult()
	errorResult, err := canonical.NewToolResult(result.ID(), result.CallID(), result.Content(), true)
	if err != nil {
		t.Fatal(err)
	}
	toolItems[1], err = canonical.NewToolResultItem(errorResult)
	if err != nil {
		t.Fatal(err)
	}
	errorRequest := rebuildRequest(t, toolRequest, func(params *canonical.RequestParams) { params.Input = toolItems })

	for _, test := range []struct {
		name, path string
		request    canonical.Request
	}{
		{"error result", "input[1].is_error", errorRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := mapRequest(test.request, "gpt-5.2")
			if execution.CodeOf(err) != execution.CodeInvalidRequest || executionErrorPath(err) != test.path {
				t.Fatalf("error = %v, code/path = %q/%q", err, execution.CodeOf(err), executionErrorPath(err))
			}
		})
	}
}

func TestMapRequestRejectsUnsupportedCanonicalSemantics(t *testing.T) {
	base := decodeCanonicalRequest(t, `{"model":"public/openai","input":"hello"}`)
	seed := int64(42)
	sampling, err := canonical.NewSampling(canonical.SamplingParams{Seed: &seed})
	if err != nil {
		t.Fatal(err)
	}
	continuation, err := canonical.NewContinuation("openai", "public/openai", []byte("opaque-private-continuation"))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		request  canonical.Request
		upstream string
		path     string
	}{
		{"empty upstream", base, "", "provider.model"},
		{"seed", rebuildRequest(t, base, func(params *canonical.RequestParams) { params.Sampling = sampling }), "gpt-5.2", "sampling.seed"},
		{"continuation", rebuildRequest(t, base, func(params *canonical.RequestParams) { params.Continuation = &continuation }), "gpt-5.2", "continuation"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := mapRequest(test.request, test.upstream)
			if execution.CodeOf(err) != execution.CodeInvalidRequest || executionErrorPath(err) != test.path {
				t.Fatalf("error = %v, code/path = %q/%q", err, execution.CodeOf(err), executionErrorPath(err))
			}
			if bytes.Contains([]byte(err.Error()), []byte("opaque-private-continuation")) {
				t.Fatalf("error leaked continuation: %v", err)
			}
		})
	}
}
