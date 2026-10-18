package chat

import (
	"bytes"
	"fmt"
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
	accumulator := NewAccumulator(responseID, "public/model", ids, nil)
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

func TestAccumulatorBoundsPendingToolArgumentsBeforeName(t *testing.T) {
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids, nil)
	fragment := strings.Repeat("a", 64<<10)
	raw := `{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"` + fragment + `"}}]}}]}`
	chunk, err := DecodeChunk([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	// No custom tool is declared, so unnamed arguments are bounded as JSON.
	for sent := 0; sent <= canonical.MaxJSONBytes; sent += len(fragment) {
		if _, err := accumulator.Accept(chunk); err != nil {
			if sent+len(fragment) <= canonical.MaxJSONBytes {
				t.Fatalf("rejected within limit after %d bytes: %v", sent, err)
			}
			return
		}
	}
	t.Fatal("unnamed tool arguments beyond canonical.MaxJSONBytes were buffered without error")
}

func TestAccumulatorBoundsDistinctToolCalls(t *testing.T) {
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids, nil)
	for index := 0; index <= canonical.MaxResponseItems; index++ {
		raw := fmt.Sprintf(`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":%d,"function":{"arguments":"{}"}}]}}]}`, index)
		chunk, err := DecodeChunk([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := accumulator.Accept(chunk); err != nil {
			if index < canonical.MaxResponseItems {
				t.Fatalf("rejected tool call %d within limit: %v", index, err)
			}
			return
		}
	}
	t.Fatal("more than canonical.MaxResponseItems distinct tool calls were tracked without error")
}

func TestEncodeMessagesGroupsParallelToolCallsIntoOneAssistantTurn(t *testing.T) {
	textBlock := func(value string) canonical.ContentBlock {
		text, err := canonical.NewTextContent(value)
		if err != nil {
			t.Fatal(err)
		}
		block, err := canonical.NewTextBlock(text)
		if err != nil {
			t.Fatal(err)
		}
		return block
	}
	messageItem := func(id byte, role string, value string) canonical.Item {
		message, err := canonical.NewMessage(fixedItemID(t, id), canonical.Role(role), []canonical.ContentBlock{textBlock(value)})
		if err != nil {
			t.Fatal(err)
		}
		item, err := canonical.NewMessageItem(message)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	ids := &countingIDs{next: 100}
	callIDs := make([]canonical.ToolCallID, 2)
	items := []canonical.Item{messageItem(1, "user", "look both up"), messageItem(2, "assistant", "checking")}
	for index := range callIDs {
		callID, err := ids.ToolCallID()
		if err != nil {
			t.Fatal(err)
		}
		callIDs[index] = callID
		call, err := canonical.NewToolCall(fixedItemID(t, byte(10+index)), callID, "lookup", mustSchema(t))
		if err != nil {
			t.Fatal(err)
		}
		item, err := canonical.NewToolCallItem(call)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	for index, callID := range callIDs {
		result, err := canonical.NewToolResult(fixedItemID(t, byte(20+index)), callID, []canonical.ContentBlock{textBlock("found")}, false)
		if err != nil {
			t.Fatal(err)
		}
		item, err := canonical.NewToolResultItem(result)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	messages, err := encodeMessages(items)
	if err != nil {
		t.Fatal(err)
	}
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message["role"].(string))
	}
	if got, want := strings.Join(roles, ","), "user,assistant,tool,tool"; got != want {
		t.Fatalf("roles = %s, want %s (every tool message must directly follow the assistant turn that issued its call)", got, want)
	}
	assistant := messages[1]
	if assistant["content"] != "checking" {
		t.Fatalf("assistant content = %#v, want preceding assistant text preserved", assistant["content"])
	}
	calls, ok := assistant["tool_calls"].([]map[string]any)
	if !ok || len(calls) != 2 || calls[0]["id"] != string(callIDs[0]) || calls[1]["id"] != string(callIDs[1]) {
		t.Fatalf("assistant tool_calls = %#v, want both calls in order", assistant["tool_calls"])
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
	response, err := BuildResponse(responseID, "deepseek-chat", completion, ids, nil)
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
	accumulator := NewAccumulator(responseID, "public/model", ids, nil)
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
	accumulator := NewAccumulator(responseID, "public/model", ids, nil)
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

func TestBuildResponseTreatsEmptyToolArgumentsAsEmptyObject(t *testing.T) {
	for _, arguments := range []string{``, ` `} {
		completion, err := DecodeCompletion([]byte(`{"id":"chatcmpl-1","model":"deepseek-chat","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_files","arguments":"` + arguments + `"}}]}}]}`))
		if err != nil {
			t.Fatal(err)
		}
		ids := &countingIDs{}
		responseID, err := ids.ResponseID()
		if err != nil {
			t.Fatal(err)
		}
		response, err := BuildResponse(responseID, "deepseek-chat", completion, ids, nil)
		if err != nil {
			t.Fatalf("BuildResponse(arguments=%q) error = %v", arguments, err)
		}
		call, ok := response.Items()[len(response.Items())-1].ToolCall()
		if !ok || call.Name() != "list_files" || string(call.Arguments().Bytes()) != `{}` {
			t.Fatalf("tool call = %#v ok=%v, want list_files with {}", call, ok)
		}
	}
	completion, err := DecodeCompletion([]byte(`{"id":"chatcmpl-1","model":"deepseek-chat","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_files","arguments":"{"}}]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ids := &countingIDs{}
	responseID, _ := ids.ResponseID()
	if _, err := BuildResponse(responseID, "deepseek-chat", completion, ids, nil); err == nil {
		t.Fatal("BuildResponse accepted truncated tool arguments")
	}
}
