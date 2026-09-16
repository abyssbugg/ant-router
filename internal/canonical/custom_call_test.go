package canonical

import (
	"strings"
	"testing"
)

func TestCustomToolCallVariants(t *testing.T) {
	direct, err := NewCustomToolCall(testItemID(t, 90), testCallID(t, 90), "apply_patch", "*** Begin Patch\n*** End Patch")
	if err != nil || !direct.Valid() || direct.Namespace() != "" || direct.Name() != "apply_patch" || direct.Input() == "" {
		t.Fatalf("direct custom call = %#v, err = %v", direct, err)
	}
	namespaced, err := NewNamespacedCustomToolCall(testItemID(t, 91), testCallID(t, 91), "workspace", "edit", "replace text")
	if err != nil || !namespaced.Valid() || namespaced.Namespace() != "workspace" || namespaced.Name() != "edit" {
		t.Fatalf("namespaced custom call = %#v, err = %v", namespaced, err)
	}
	item, err := NewCustomToolCallItem(namespaced)
	if err != nil || !item.Valid() || item.Type() != ItemType("custom_tool_call") || item.ID() != namespaced.ID() {
		t.Fatalf("custom item = %#v, err = %v", item, err)
	}
	got, ok := item.CustomToolCall()
	if !ok || got.CallID() != namespaced.CallID() || got.Input() != namespaced.Input() {
		t.Fatalf("custom accessor = %#v, %v", got, ok)
	}
	if _, ok := item.ToolCall(); ok {
		t.Fatal("custom call exposed as function call")
	}
}

func TestCustomToolCallRejectsInvalid(t *testing.T) {
	validID := testItemID(t, 92)
	validCallID := testCallID(t, 92)
	tests := []struct {
		name string
		make func() error
		code ErrorCode
	}{
		{"item id", func() error { _, err := NewCustomToolCall("", validCallID, "edit", "input"); return err }, CodeInvalidID},
		{"call id", func() error { _, err := NewCustomToolCall(validID, "", "edit", "input"); return err }, CodeInvalidID},
		{"name", func() error { _, err := NewCustomToolCall(validID, validCallID, "bad name", "input"); return err }, CodeInvalidInvariant},
		{"namespace", func() error {
			_, err := NewNamespacedCustomToolCall(validID, validCallID, "bad namespace", "edit", "input")
			return err
		}, CodeInvalidInvariant},
		{"utf8", func() error {
			_, err := NewCustomToolCall(validID, validCallID, "edit", string([]byte{0xff}))
			return err
		}, CodeInvalidUTF8},
		{"control", func() error { _, err := NewCustomToolCall(validID, validCallID, "edit", "bad\x00input"); return err }, CodeInvalidInvariant},
		{"limit", func() error {
			_, err := NewCustomToolCall(validID, validCallID, "edit", strings.Repeat("x", MaxCustomToolInputBytes+1))
			return err
		}, CodeLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.make(); codeOfCanonical(err) != test.code {
				t.Fatalf("code = %q, want %q; error = %v", codeOfCanonical(err), test.code, err)
			}
		})
	}
	if _, err := NewCustomToolCallItem(CustomToolCall{}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Fatal("zero custom call item accepted")
	}
}

func TestRequestCorrelatesCustomCallsAndResults(t *testing.T) {
	definition := testCustomTool(t, "apply_patch")
	call, _ := NewCustomToolCall(testItemID(t, 93), testCallID(t, 93), "apply_patch", "patch")
	callItem, _ := NewCustomToolCallItem(call)
	result, _ := NewToolResult(testItemID(t, 94), call.CallID(), []ContentBlock{testTextBlock(t, "done")}, false)
	resultItem, _ := NewToolResultItem(result)
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 93), Model: "model", Input: []Item{callItem, resultItem}, Tools: []ToolDefinition{definition}})
	if err != nil || !request.Valid() {
		t.Fatalf("custom request = %#v, err = %v", request, err)
	}
	for _, capability := range []Capability{CapabilityTools, CapabilityCustomTools, CapabilityText} {
		if !request.Capabilities().Has(capability) {
			t.Fatalf("capability %q missing", capability)
		}
	}
}

func TestRequestCorrelatesNamespacedCustomCall(t *testing.T) {
	custom := testCustomTool(t, "edit")
	namespace, err := NewToolNamespace("workspace", "", []ToolDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	call, _ := NewNamespacedCustomToolCall(testItemID(t, 97), testCallID(t, 97), "workspace", "edit", "input")
	callItem, _ := NewCustomToolCallItem(call)
	result, _ := NewToolResult(testItemID(t, 98), call.CallID(), []ContentBlock{testTextBlock(t, "done")}, false)
	resultItem, _ := NewToolResultItem(result)
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 97), Model: "model", Input: []Item{callItem, resultItem}, Tools: []ToolDefinition{namespace}})
	if err != nil || !request.Capabilities().Has(CapabilityNamespaceTools) {
		t.Fatalf("namespaced custom replay = %#v, err = %v", request, err)
	}
}

func TestRequestRejectsDuplicateCallIDAcrossFunctionAndCustom(t *testing.T) {
	function := testNamespaceFunction(t, "read")
	custom := testCustomTool(t, "edit")
	callID := testCallID(t, 99)
	functionCall, _ := NewToolCall(testItemID(t, 99), callID, "read", testJSONObject(t, `{}`))
	functionItem, _ := NewToolCallItem(functionCall)
	customCall, _ := NewCustomToolCall(testItemID(t, 100), callID, "edit", "input")
	customItem, _ := NewCustomToolCallItem(customCall)
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 99), Model: "model", Input: []Item{functionItem, customItem}, Tools: []ToolDefinition{function, custom}}); err == nil {
		t.Fatal("duplicate call ID across function and custom accepted")
	}
}

func TestResponseTreatsCustomCallAsToolCall(t *testing.T) {
	call, _ := NewCustomToolCall(testItemID(t, 104), testCallID(t, 104), "apply_patch", "patch")
	item, _ := NewCustomToolCallItem(call)
	response, err := NewResponse(ResponseParams{ID: testResponseID(t, 104), Model: "model", Items: []Item{item}, FinishReason: FinishReason("tool_call")})
	if err != nil || !response.Valid() {
		t.Fatalf("custom tool response = %#v, err = %v", response, err)
	}
	if _, err := NewResponse(ResponseParams{ID: testResponseID(t, 105), Model: "model", Items: []Item{item}, FinishReason: FinishReason("stop")}); err == nil {
		t.Fatal("custom tool response accepted with stop finish reason")
	}
}

func TestResponseRejectsDuplicateCallIDAcrossFunctionAndCustom(t *testing.T) {
	callID := testCallID(t, 106)
	functionCall, _ := NewToolCall(testItemID(t, 106), callID, "read", testJSONObject(t, `{}`))
	functionItem, _ := NewToolCallItem(functionCall)
	customCall, _ := NewCustomToolCall(testItemID(t, 107), callID, "edit", "input")
	customItem, _ := NewCustomToolCallItem(customCall)
	if _, err := NewResponse(ResponseParams{ID: testResponseID(t, 106), Model: "model", Items: []Item{functionItem, customItem}, FinishReason: FinishReason("tool_call")}); err == nil {
		t.Fatal("duplicate response call ID across variants accepted")
	}
}

func FuzzCustomToolCall(f *testing.F) {
	f.Add("", "apply_patch", "*** Begin Patch\n*** End Patch")
	f.Add("workspace", "edit", "bad\x00input")
	f.Fuzz(func(t *testing.T, namespace, name, input string) {
		if len(namespace) > MaxToolNameBytes+1 || len(name) > MaxToolNameBytes+1 || len(input) > MaxCustomToolInputBytes+1 {
			return
		}
		var call CustomToolCall
		var err error
		if namespace == "" {
			call, err = NewCustomToolCall(testItemID(t, 101), testCallID(t, 101), name, input)
		} else {
			call, err = NewNamespacedCustomToolCall(testItemID(t, 101), testCallID(t, 101), namespace, name, input)
		}
		if err == nil && !call.Valid() {
			t.Fatal("constructor returned invalid custom call")
		}
	})
}

func TestRequestRequiresMatchingToolDefinitionKind(t *testing.T) {
	function := testNamespaceFunction(t, "edit")
	custom := testCustomTool(t, "apply_patch")
	customCall, _ := NewCustomToolCall(testItemID(t, 95), testCallID(t, 95), "edit", "input")
	customItem, _ := NewCustomToolCallItem(customCall)
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 95), Model: "model", Input: []Item{customItem}, Tools: []ToolDefinition{function}}); err == nil {
		t.Fatal("custom call accepted for function declaration")
	}
	functionCall, _ := NewToolCall(testItemID(t, 96), testCallID(t, 96), "apply_patch", testJSONObject(t, `{}`))
	functionItem, _ := NewToolCallItem(functionCall)
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 96), Model: "model", Input: []Item{functionItem}, Tools: []ToolDefinition{custom}}); err == nil {
		t.Fatal("function call accepted for custom declaration")
	}
}
