package chat

import (
	"bytes"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type countingIDs struct{ next byte }

func (c *countingIDs) nextID() *bytes.Reader {
	c.next++
	return bytes.NewReader(bytes.Repeat([]byte{c.next}, canonical.IDRandomBytes))
}
func (c *countingIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(c.nextID())
}
func (c *countingIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(c.nextID())
}
func (c *countingIDs) EventID() (canonical.EventID, error) {
	return canonical.GenerateEventID(c.nextID())
}
func (c *countingIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.GenerateResponseID(c.nextID())
}

func fixedItemID(t *testing.T, value byte) canonical.ItemID {
	t.Helper()
	id, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{value}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fixedRequestID(t *testing.T, value byte) canonical.RequestID {
	t.Helper()
	id, err := canonical.GenerateRequestID(bytes.NewReader(bytes.Repeat([]byte{value}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustSchema(t *testing.T) canonicaljson.Value {
	t.Helper()
	schema, err := canonicaljson.Parse([]byte(`{"type":"object"}`), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func testRequest(t *testing.T) canonical.Request {
	t.Helper()
	text, err := canonical.NewTextContent("inspect the payload")
	if err != nil {
		t.Fatal(err)
	}
	userBlock, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}
	userMessage, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{userBlock})
	if err != nil {
		t.Fatal(err)
	}
	userItem, err := canonical.NewMessageItem(userMessage)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := canonical.NewToolDefinition("lookup", "finds payloads", mustSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	toolChoice, err := canonical.NewToolChoice(canonical.ToolChoiceKind("specific"), "lookup")
	if err != nil {
		t.Fatal(err)
	}
	output, err := canonical.NewOutputConstraints(canonical.OutputParams{Format: canonical.OutputFormat("json_schema"), SchemaName: "payload", Schema: mustSchema(t)})
	if err != nil {
		t.Fatal(err)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:         fixedRequestID(t, 1),
		Model:      "deepseek-chat",
		Input:      []canonical.Item{userItem},
		Tools:      []canonical.ToolDefinition{tool},
		ToolChoice: toolChoice,
		Output:     output,
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestEncodeRequestShape(t *testing.T) {
	body, err := EncodeRequest(testRequest(t), "deepseek-chat", false)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(body)
	for _, want := range []string{
		`"model":"deepseek-chat"`,
		`"role":"user"`,
		`"content":"inspect the payload"`,
		`"name":"lookup"`,
		`"tool_choice":{"function":{"name":"lookup"},"type":"function"}`,
		`"response_format":{"json_schema":{"name":"payload"`,
		`"parallel_tool_calls":false`,
		`"stream":false`,
	} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("encoded body missing %s: %s", want, encoded)
		}
	}
	if strings.Contains(encoded, "continuation") {
		t.Fatal("encoded body leaked continuation state")
	}
}

func TestEncodeRequestUsesUpstreamModel(t *testing.T) {
	body, err := EncodeRequest(testRequest(t), "deepseek-v3-official", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"model":"deepseek-v3-official"`) {
		t.Fatalf("body missing upstream model: %s", body)
	}
	if strings.Contains(string(body), `"model":"deepseek-chat"`) {
		t.Fatal("body leaked the public alias as the upstream model")
	}
	if _, err := EncodeRequest(testRequest(t), "  ", false); err == nil {
		t.Fatal("empty upstream model accepted")
	}
}

func TestAccumulatorDefersToolStartUntilName(t *testing.T) {
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids)
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	chunks := []string{
		`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"up_1","function":{"name":"lookup"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
	}
	for _, raw := range chunks {
		chunk, err := DecodeChunk([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		events, err := accumulator.Accept(chunk)
		if err != nil {
			t.Fatalf("chunk %s: %v", raw, err)
		}
		for _, event := range events {
			if _, err := machine.Accept(event); err != nil {
				t.Fatalf("machine rejected %q: %v", event.Type(), err)
			}
		}
	}
	finalEvents, err := accumulator.Finish()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range finalEvents {
		if _, err := machine.Accept(event); err != nil {
			t.Fatalf("machine rejected final %q: %v", event.Type(), err)
		}
	}
}

func TestEncodeRequestRejectsContinuation(t *testing.T) {
	request := testRequest(t)
	continuation, err := canonical.NewContinuation("openai", "gpt-test", []byte(`{"entries":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := canonical.NewRequest(canonical.RequestParams{
		ID: request.ID(), Model: request.Model(), Input: request.Input(),
		Tools: request.Tools(), ToolChoice: request.ToolChoice(),
		Output: request.Output(), Sampling: request.Sampling(),
		Continuation: &continuation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeRequest(rebuilt, "deepseek-chat", false); !IsUnsupported(err) {
		t.Fatalf("continuation error = %v, want unsupported", err)
	}
}

func TestEncodeRequestRejectsHostedSearchTool(t *testing.T) {
	options, err := canonical.NewHostedWebSearchOptions(canonical.HostedWebSearchOptionsParams{})
	if err != nil {
		t.Fatal(err)
	}
	webSearch, err := canonical.NewHostedWebSearchToolWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest(t)
	rebuilt, err := canonical.NewRequest(canonical.RequestParams{
		ID: request.ID(), Model: request.Model(), Input: request.Input(),
		Tools: []canonical.ToolDefinition{webSearch},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeRequest(rebuilt, "deepseek-chat", false); !IsUnsupported(err) {
		t.Fatalf("web search error = %v, want unsupported", err)
	}
}

func TestBuildResponseFromCompletion(t *testing.T) {
	completion, err := DecodeCompletion([]byte(`{
		"id":"chatcmpl-1","model":"deepseek-chat",
		"choices":[{"index":0,"finish_reason":"tool_calls","message":{
			"role":"assistant","content":"checking",
			"reasoning_content":"thinking hard",
			"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]
		}}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	response, err := BuildResponse(responseID, "deepseek-chat", completion, ids)
	if err != nil {
		t.Fatal(err)
	}
	if response.FinishReason() != canonical.FinishReason("tool_call") || len(response.Items()) != 3 {
		t.Fatalf("response = finish %q items %d", response.FinishReason(), len(response.Items()))
	}
	if response.Items()[0].Type() != canonical.ItemType("reasoning_summary") {
		t.Fatalf("item[0] = %q, want reasoning_summary", response.Items()[0].Type())
	}
	if response.Items()[1].Type() != canonical.ItemType("message") {
		t.Fatalf("item[1] = %q, want message", response.Items()[1].Type())
	}
	if response.Items()[2].Type() != canonical.ItemType("tool_call") {
		t.Fatalf("item[2] = %q, want tool_call", response.Items()[2].Type())
	}
	if response.Usage().TotalTokens() != 15 {
		t.Fatalf("usage = %#v", response.Usage())
	}
}

func TestAccumulatorStreamReconstructsThroughMachine(t *testing.T) {
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids)
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	chunks := []string{
		`{"id":"c1","model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":"step one"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"lookup","arguments":""}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"lookup","arguments":"{}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
		`{"id":"c1","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
	}
	var terminal canonical.Event
	for _, raw := range chunks {
		chunk, err := DecodeChunk([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		events, err := accumulator.Accept(chunk)
		if err != nil {
			t.Fatalf("chunk %s: %v", raw, err)
		}
		for _, event := range events {
			if _, err := machine.Accept(event); err != nil {
				t.Fatalf("machine rejected %q: %v", event.Type(), err)
			}
		}
	}
	finalEvents, err := accumulator.Finish()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range finalEvents {
		if _, err := machine.Accept(event); err != nil {
			t.Fatalf("machine rejected final %q: %v", event.Type(), err)
		}
		if event.Type() == canonical.EventType("response_completed") {
			terminal = event
		}
	}
	if machine.State() != streaming.StateTerminal {
		t.Fatalf("machine state = %q", machine.State())
	}
	completed, ok := terminal.ResponseCompleted()
	if !ok {
		t.Fatal("terminal event missing")
	}
	if completed.FinishReason() != canonical.FinishReason("tool_call") {
		t.Fatalf("finish = %q", completed.FinishReason())
	}
	if len(completed.ItemIDs()) != 4 {
		t.Fatalf("item ids = %d, want 4 (reasoning, message, two tool calls)", len(completed.ItemIDs()))
	}
	if completed.Usage().TotalTokens() != 10 {
		t.Fatalf("usage = %#v", completed.Usage())
	}
}

func TestAccumulatorRejectsBrokenArguments(t *testing.T) {
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids)
	for _, raw := range []string{
		`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":"{not json"}}]}}]}`,
	} {
		chunk, err := DecodeChunk([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := accumulator.Accept(chunk); err != nil {
			t.Fatalf("chunk %s: %v", raw, err)
		}
	}
	if _, err := accumulator.Finish(); err == nil {
		t.Fatal("Finish() accepted broken tool arguments")
	}
}

func TestDecodeChunkRejectsErrorPayload(t *testing.T) {
	_, err := DecodeChunk([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"429"}}`))
	if err == nil || !strings.Contains(err.Error(), "upstream_error") {
		t.Fatalf("error chunk = %v", err)
	}
}
