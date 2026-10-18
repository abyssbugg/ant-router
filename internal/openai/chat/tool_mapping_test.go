package chat

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

const mappingPatch = "*** Begin Patch\n\"quoted\" \\ naïve 🚀\n*** End Patch"

func mappingNameMap() ToolNameMap {
	return ToolNameMap{
		"apply_patch":        {Kind: canonical.ToolDefinitionKindCustom, Name: "apply_patch"},
		"editor__write_file": {Kind: canonical.ToolDefinitionKindCustom, Name: "write_file", Namespace: "editor"},
		"crm__lookup":        {Kind: canonical.ToolDefinitionKindFunction, Name: "lookup", Namespace: "crm"},
		"lookup":             {Kind: canonical.ToolDefinitionKindFunction, Name: "lookup"},
		"unmapped__tool":     {Kind: canonical.ToolDefinitionKindFunction, Name: "unmapped__tool"},
	}
}

func customArguments(t *testing.T, input string) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]string{"input": input})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

type wantMappedItem struct {
	kind      canonical.ItemType
	namespace string
	name      string
	input     string
	arguments string
}

func assertMappedItems(t *testing.T, items []canonical.Item, want []wantMappedItem) {
	t.Helper()
	if len(items) != len(want) {
		t.Fatalf("items = %d, want %d", len(items), len(want))
	}
	for index, item := range items {
		expected := want[index]
		if item.Type() != expected.kind {
			t.Fatalf("item %d type = %q, want %q", index, item.Type(), expected.kind)
		}
		switch expected.kind {
		case canonical.ItemType("custom_tool_call"):
			call, _ := item.CustomToolCall()
			if call.Namespace() != expected.namespace || call.Name() != expected.name || call.Input() != expected.input {
				t.Fatalf("item %d custom = %q/%q input %q", index, call.Namespace(), call.Name(), call.Input())
			}
		case canonical.ItemType("tool_call"):
			call, _ := item.ToolCall()
			if call.Namespace() != expected.namespace || call.Name() != expected.name || string(call.Arguments().Bytes()) != expected.arguments {
				t.Fatalf("item %d call = %q/%q args %s", index, call.Namespace(), call.Name(), call.Arguments().Bytes())
			}
		}
	}
}

func mappingWant() []wantMappedItem {
	return []wantMappedItem{
		{kind: canonical.ItemType("custom_tool_call"), name: "apply_patch", input: mappingPatch},
		{kind: canonical.ItemType("custom_tool_call"), namespace: "editor", name: "write_file", input: ""},
		{kind: canonical.ItemType("tool_call"), namespace: "crm", name: "lookup", arguments: `{"id":7}`},
		{kind: canonical.ItemType("tool_call"), name: "lookup", arguments: `{"q":"x"}`},
		{kind: canonical.ItemType("tool_call"), name: "unmapped__tool", arguments: `{}`},
	}
}

func TestBuildResponseMapsCustomAndNamespacedCalls(t *testing.T) {
	calls := []map[string]any{
		{"id": "c0", "type": "function", "function": map[string]any{"name": "apply_patch", "arguments": customArguments(t, mappingPatch)}},
		{"id": "c1", "type": "function", "function": map[string]any{"name": "editor__write_file", "arguments": `{"input":""}`}},
		{"id": "c2", "type": "function", "function": map[string]any{"name": "crm__lookup", "arguments": `{"id":7}`}},
		{"id": "c3", "type": "function", "function": map[string]any{"name": "lookup", "arguments": `{"q":"x"}`}},
		{"id": "c4", "type": "function", "function": map[string]any{"name": "unmapped__tool", "arguments": `{}`}},
	}
	raw, err := json.Marshal(map[string]any{
		"id": "chatcmpl-1", "model": "m",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": calls}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := DecodeCompletion(raw)
	if err != nil {
		t.Fatal(err)
	}
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	response, err := BuildResponse(responseID, "public/model", completion, ids, mappingNameMap())
	if err != nil {
		t.Fatal(err)
	}
	assertMappedItems(t, response.Items(), mappingWant())
}

func streamMappingChunks(t *testing.T) []string {
	t.Helper()
	patchArgs := customArguments(t, mappingPatch)
	split := strings.Index(patchArgs, `\"`) + 1 // split inside an escape sequence
	toolChunk := func(index int, id, name, arguments string) string {
		function := map[string]any{"arguments": arguments}
		if name != "" {
			function["name"] = name
		}
		call := map[string]any{"index": index, "function": function}
		if id != "" {
			call["id"] = id
		}
		encoded, err := json.Marshal(map[string]any{"id": "c1", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}})
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	return []string{
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"working"}}]}`,
		toolChunk(0, "call_0", "apply_patch", patchArgs[:split]),
		toolChunk(0, "", "", patchArgs[split:]),
		toolChunk(1, "call_1", "editor__write_file", `{"input":`),
		toolChunk(1, "", "", `""}`),
		toolChunk(2, "call_2", "crm__lookup", `{"id":`),
		toolChunk(2, "", "", `7}`),
		toolChunk(3, "call_3", "lookup", `{"q":"x"}`),
		toolChunk(4, "call_4", "unmapped__tool", `{}`),
		`{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
	}
}

func runMappingStream(t *testing.T, chunks []string, nameMap ToolNameMap) (canonical.Response, []canonical.Event, error) {
	t.Helper()
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids, nameMap)
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	var all []canonical.Event
	var response canonical.Response
	feed := func(events []canonical.Event) {
		for _, event := range events {
			all = append(all, event)
			transition, err := machine.Accept(event)
			if err != nil {
				t.Fatalf("machine rejected %q: %v", event.Type(), err)
			}
			if value, ok := transition.Response(); ok {
				response = value
			}
		}
	}
	for _, raw := range chunks {
		chunk, err := DecodeChunk([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		events, err := accumulator.Accept(chunk)
		if err != nil {
			return canonical.Response{}, all, err
		}
		feed(events)
	}
	events, err := accumulator.Finish()
	if err != nil {
		return canonical.Response{}, all, err
	}
	feed(events)
	if machine.State() != streaming.StateTerminal {
		t.Fatalf("machine state = %q", machine.State())
	}
	return response, all, nil
}

func TestAccumulatorMapsCustomAndNamespacedCallsThroughMachine(t *testing.T) {
	response, events, err := runMappingStream(t, streamMappingChunks(t), mappingNameMap())
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 6 || items[0].Type() != canonical.ItemType("message") {
		t.Fatalf("items = %d, first %q", len(items), items[0].Type())
	}
	assertMappedItems(t, items[1:], mappingWant())
	for _, event := range events {
		if delta, ok := event.ToolArgumentsDelta(); ok {
			for _, item := range items[1:3] {
				if delta.ItemID() == item.ID() {
					t.Fatalf("custom call %s must not stream raw JSON argument deltas", item.ID())
				}
			}
		}
	}
}

func TestAccumulatorWithoutNameMapKeepsPlainCalls(t *testing.T) {
	response, _, err := runMappingStream(t, streamMappingChunks(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	// Expect initial message + 5 tool calls (from mappingWant())
	if len(items) < 6 {
		t.Fatalf("expected at least 6 items (1 message + 5 calls), got %d", len(items))
	}
	for index, item := range items[1:] {
		call, ok := item.ToolCall()
		if !ok || call.Namespace() != "" {
			t.Fatalf("item %d = %q namespace %q; nil map must keep plain function calls", index, item.Type(), call.Namespace())
		}
	}
}

func TestCustomArgumentsMustBeExactInputObject(t *testing.T) {
	bad := []string{
		`not json`, `[]`, `"text"`, `{}`, `{"input":1}`, `{"input":null}`, `{"input":"a","extra":1}`,
		`{"input":"a","input":"b"}`, `{"Input":"a"}`, `{"input":"a"} {}`,
	}
	for _, arguments := range bad {
		if _, err := decodeCustomInput(arguments); err == nil {
			t.Errorf("decodeCustomInput(%q) accepted invalid arguments", arguments)
		}
		encodedArgs, _ := json.Marshal(arguments)
		chunks := []string{
			`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_0","function":{"name":"apply_patch","arguments":` + string(encodedArgs) + `}}]}}]}`,
			`{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
		}
		if _, _, err := runMappingStream(t, chunks, mappingNameMap()); err == nil {
			t.Errorf("stream accepted invalid custom arguments %q", arguments)
		}
		completion := Completion{Choices: []ChoiceWire{{FinishReason: "tool_calls"}}}
		completion.Choices[0].Message.Role = "assistant"
		completion.Choices[0].Message.ToolCalls = []ToolCallWire{{ID: "c", Type: "function"}}
		completion.Choices[0].Message.ToolCalls[0].Function.Name = "apply_patch"
		completion.Choices[0].Message.ToolCalls[0].Function.Arguments = arguments
		ids := &countingIDs{}
		responseID, _ := ids.ResponseID()
		if _, err := BuildResponse(responseID, "m", completion, ids, mappingNameMap()); err == nil {
			t.Errorf("BuildResponse accepted invalid custom arguments %q", arguments)
		}
	}
	if input, err := decodeCustomInput(` { "input" : "ok" } `); err != nil || input != "ok" {
		t.Fatalf("whitespace-tolerant decode = %q, %v", input, err)
	}
}

// TestAccumulatorBoundsTotalRetainedToolArguments proves the per-stream
// bound: many concurrent calls that each stay under the per-call limit must
// not let an untrusted upstream make the accumulator retain unbounded memory.
func TestAccumulatorBoundsTotalRetainedToolArguments(t *testing.T) {
	fragment := strings.Repeat("a", 256<<10)
	perCall := canonical.MaxJSONBytes/len(fragment) - 1
	calls := maxRetainedToolArgumentBytes/(perCall*len(fragment)) + 2
	chunks := []string{`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant"}}]}`}
	for call := 0; call < calls; call++ {
		index := strconv.Itoa(call)
		chunks = append(chunks, `{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":`+index+`,"id":"call_`+index+`","function":{"name":"lookup","arguments":""}}]}}]}`)
		for written := 0; written < perCall; written++ {
			chunks = append(chunks, `{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":`+index+`,"function":{"arguments":"`+fragment+`"}}]}}]}`)
		}
	}
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids, nil)
	for _, raw := range chunks {
		chunk, decodeErr := DecodeChunk([]byte(raw))
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if _, err = accumulator.Accept(chunk); err != nil {
			break
		}
	}
	if err == nil || !strings.Contains(err.Error(), "arguments_total_limit") {
		t.Fatalf("err = %v, want arguments_total_limit protocol error", err)
	}
}

// TestAccumulatorCountsReplayedPreNameArgumentsOnce fills the per-stream
// bound exactly. One call's arguments arrive before its name and are replayed
// after the item starts; counting them twice would falsely reject the stream.
func TestAccumulatorCountsReplayedPreNameArgumentsOnce(t *testing.T) {
	fragment := strings.Repeat("a", 512<<10)
	calls := maxRetainedToolArgumentBytes / len(fragment)
	chunks := []string{
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_0","function":{"arguments":"` + fragment + `"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup"}}]}}]}`,
	}
	for call := 1; call < calls; call++ {
		index := strconv.Itoa(call)
		chunks = append(chunks, `{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":`+index+`,"id":"call_`+index+`","function":{"name":"lookup","arguments":"`+fragment+`"}}]}}]}`)
	}
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids, nil)
	for position, raw := range chunks {
		chunk, decodeErr := DecodeChunk([]byte(raw))
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if _, err := accumulator.Accept(chunk); err != nil {
			t.Fatalf("chunk %d rejected at exactly the bound: %v", position, err)
		}
	}
}

func TestAccumulatorBoundsBufferedCustomArguments(t *testing.T) {
	fragment := strings.Repeat("a", 64<<10)
	chunks := []string{`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_0","function":{"name":"apply_patch","arguments":"{\"input\":\""}}]}}]}`}
	// Custom tool arguments are now bounded by MaxRequestBytes (2 MiB) to support larger inputs
	for written := 0; written <= canonical.MaxRequestBytes; written += len(fragment) {
		chunks = append(chunks, `{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"`+fragment+`"}}]}}]}`)
	}
	_, _, err := runMappingStream(t, chunks, mappingNameMap())
	if err == nil || !strings.Contains(err.Error(), "arguments_limit") {
		t.Fatalf("err = %v, want arguments_limit protocol error", err)
	}
}
