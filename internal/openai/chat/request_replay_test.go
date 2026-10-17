package chat

import (
	"encoding/json"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func replayCallID(t *testing.T, next byte) canonical.ToolCallID {
	t.Helper()
	id, err := (&countingIDs{next: next}).ToolCallID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func replayCustomItem(t *testing.T, itemByte byte, callID canonical.ToolCallID, namespace, name, input string) canonical.Item {
	t.Helper()
	var call canonical.CustomToolCall
	var err error
	if namespace == "" {
		call, err = canonical.NewCustomToolCall(fixedItemID(t, itemByte), callID, name, input)
	} else {
		call, err = canonical.NewNamespacedCustomToolCall(fixedItemID(t, itemByte), callID, namespace, name, input)
	}
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewCustomToolCallItem(call)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func replayToolItem(t *testing.T, itemByte byte, callID canonical.ToolCallID, namespace, name string) canonical.Item {
	t.Helper()
	var call canonical.ToolCall
	var err error
	if namespace == "" {
		call, err = canonical.NewToolCall(fixedItemID(t, itemByte), callID, name, mustSchema(t))
	} else {
		call, err = canonical.NewNamespacedToolCall(fixedItemID(t, itemByte), callID, namespace, name, mustSchema(t))
	}
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolCallItem(call)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func replayToolCalls(t *testing.T, message map[string]any) []map[string]any {
	t.Helper()
	calls, ok := message["tool_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("message has no tool_calls: %#v", message)
	}
	return calls
}

func replayFunction(t *testing.T, call map[string]any) (string, string) {
	t.Helper()
	function := call["function"].(map[string]any)
	return function["name"].(string), function["arguments"].(string)
}

func TestReplayNamespacedToolCallUsesFlattenedName(t *testing.T) {
	callID := replayCallID(t, 100)
	messages, err := encodeMessages([]canonical.Item{replayToolItem(t, 10, callID, "crm", "lookup")})
	if err != nil {
		t.Fatal(err)
	}
	calls := replayToolCalls(t, messages[0])
	name, arguments := replayFunction(t, calls[0])
	if name != "crm__lookup" || arguments != string(mustSchema(t).Bytes()) || calls[0]["id"] != string(callID) {
		t.Fatalf("call = %#v", calls[0])
	}
}

func TestReplayCustomToolCallEncodesInputArguments(t *testing.T) {
	inputs := []string{"x", `say "hi"`, "line1\nline2\ttab", `C:\path\to\file`, "naïve — 日本語 🚀", ""}
	for index, input := range inputs {
		callID := replayCallID(t, byte(110+index))
		for _, namespace := range []string{"", "editor"} {
			messages, err := encodeMessages([]canonical.Item{replayCustomItem(t, byte(10+index), callID, namespace, "apply_patch", input)})
			if err != nil {
				t.Fatalf("input %q namespace %q: %v", input, namespace, err)
			}
			if len(messages) != 1 || messages[0]["role"] != "assistant" {
				t.Fatalf("messages = %#v", messages)
			}
			calls := replayToolCalls(t, messages[0])
			name, arguments := replayFunction(t, calls[0])
			wantName := "apply_patch"
			if namespace != "" {
				wantName = namespace + "__apply_patch"
			}
			if name != wantName || calls[0]["type"] != "function" || calls[0]["id"] != string(callID) {
				t.Fatalf("call = %#v", calls[0])
			}
			var decoded map[string]any
			if err := json.Unmarshal([]byte(arguments), &decoded); err != nil {
				t.Fatalf("arguments %q are not JSON: %v", arguments, err)
			}
			if len(decoded) != 1 || decoded["input"] != input {
				t.Fatalf("arguments %q do not round-trip input %q", arguments, input)
			}
		}
	}
	messages, err := encodeMessages([]canonical.Item{replayCustomItem(t, 40, replayCallID(t, 130), "", "apply_patch", "x")})
	if err != nil {
		t.Fatal(err)
	}
	if _, arguments := replayFunction(t, replayToolCalls(t, messages[0])[0]); arguments != `{"input":"x"}` {
		t.Fatalf("arguments = %q, want exact {\"input\":\"x\"}", arguments)
	}
}

func TestReplayMixedToolCallsShareOneAssistantTurn(t *testing.T) {
	text, err := canonical.NewTextContent("working on it")
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("assistant"), []canonical.ContentBlock{block})
	if err != nil {
		t.Fatal(err)
	}
	assistantItem, err := canonical.NewMessageItem(assistant)
	if err != nil {
		t.Fatal(err)
	}
	callIDs := []canonical.ToolCallID{replayCallID(t, 140), replayCallID(t, 141), replayCallID(t, 142)}
	items := []canonical.Item{
		assistantItem,
		replayCustomItem(t, 10, callIDs[0], "", "apply_patch", "patch"),
		replayToolItem(t, 11, callIDs[1], "", "lookup"),
		replayToolItem(t, 12, callIDs[2], "crm", "lookup"),
	}
	for index, callID := range callIDs {
		result, err := canonical.NewToolResult(fixedItemID(t, byte(20+index)), callID, []canonical.ContentBlock{block}, false)
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
	if len(messages) != 4 || messages[0]["role"] != "assistant" || messages[0]["content"] != "working on it" {
		t.Fatalf("messages = %#v", messages)
	}
	calls := replayToolCalls(t, messages[0])
	wantNames := []string{"apply_patch", "lookup", "crm__lookup"}
	if len(calls) != len(wantNames) {
		t.Fatalf("calls = %#v", calls)
	}
	for index, call := range calls {
		if name, _ := replayFunction(t, call); name != wantNames[index] || call["id"] != string(callIDs[index]) {
			t.Fatalf("call %d = %#v", index, call)
		}
		tool := messages[index+1]
		if tool["role"] != "tool" || tool["tool_call_id"] != string(callIDs[index]) {
			t.Fatalf("tool message %d = %#v", index, tool)
		}
	}
}

func TestToolChoiceResolvesThroughNameMap(t *testing.T) {
	nameMap := ToolNameMap{
		"lookup":      {Kind: canonical.ToolDefinitionKindFunction, Name: "lookup"},
		"apply_patch": {Kind: canonical.ToolDefinitionKindCustom, Name: "apply_patch"},
		"crm__lookup": {Kind: canonical.ToolDefinitionKindFunction, Name: "lookup", Namespace: "crm"},
	}
	specific := func(namespace, name string) canonical.ToolChoice {
		t.Helper()
		var choice canonical.ToolChoice
		var err error
		if namespace == "" {
			choice, err = canonical.NewToolChoice(canonical.ToolChoiceKind("specific"), name)
		} else {
			choice, err = canonical.NewNamespacedToolChoice(namespace, name)
		}
		if err != nil {
			t.Fatal(err)
		}
		return choice
	}
	for _, tc := range []struct{ namespace, name, want string }{
		{"", "lookup", "lookup"},
		{"", "apply_patch", "apply_patch"},
		{"crm", "lookup", "crm__lookup"},
	} {
		encoded, err := encodeToolChoice(specific(tc.namespace, tc.name), nameMap)
		if err != nil {
			t.Fatalf("%s/%s: %v", tc.namespace, tc.name, err)
		}
		raw, _ := json.Marshal(encoded)
		if want := `{"function":{"name":"` + tc.want + `"},"type":"function"}`; string(raw) != want {
			t.Fatalf("%s/%s = %s, want %s", tc.namespace, tc.name, raw, want)
		}
	}
	for _, unknown := range []canonical.ToolChoice{specific("", "missing"), specific("other", "lookup"), specific("crm", "apply_patch")} {
		_, err := encodeToolChoice(unknown, nameMap)
		if err == nil || IsUnsupported(err) {
			t.Fatalf("choice %s/%s: err = %v, want invalid (not unsupported)", unknown.Namespace(), unknown.Name(), err)
		}
	}
	for _, kind := range []string{"auto", "none", "required"} {
		choice, err := canonical.NewToolChoice(canonical.ToolChoiceKind(kind), "")
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := encodeToolChoice(choice, nameMap)
		if err != nil || encoded != kind {
			t.Fatalf("%s = %v, %v", kind, encoded, err)
		}
	}
}
