package canonical

import (
	"fmt"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func testNamespaceFunction(t *testing.T, name string) ToolDefinition {
	t.Helper()
	schema, err := canonicaljson.Parse([]byte(`{"type":"object"}`), canonicaljson.Limits{MaximumBytes: MaxToolSchemaBytes, MaximumDepth: MaxJSONDepth})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := NewToolDefinition(name, "nested function", schema)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestToolNamespaceIsImmutableAndFunctionOnly(t *testing.T) {
	child := testNamespaceFunction(t, "curr_time")
	namespace, err := NewToolNamespace("clock", "Time tools", []ToolDefinition{child})
	if err != nil {
		t.Fatal(err)
	}
	if namespace.Kind() != ToolDefinitionKindNamespace || namespace.Name() != "clock" {
		t.Fatalf("namespace identity mismatch")
	}
	children := namespace.Tools()
	if len(children) != 1 || children[0].Name() != "curr_time" {
		t.Fatalf("namespace children mismatch")
	}
	children[0] = ToolDefinition{}
	if !namespace.Valid() || namespace.Tools()[0].Name() != "curr_time" {
		t.Fatal("namespace was not immutable")
	}
	if _, err := NewToolNamespace("outer", "", []ToolDefinition{namespace}); err == nil {
		t.Fatal("nested namespace accepted")
	}
}

func TestNamespacedToolChoiceAndCall(t *testing.T) {
	choice, err := NewNamespacedToolChoice("clock", "curr_time")
	if err != nil {
		t.Fatal(err)
	}
	if choice.Namespace() != "clock" || choice.Name() != "curr_time" || !choice.Valid() {
		t.Fatal("choice identity mismatch")
	}
	args, _ := canonicaljson.Parse([]byte(`{}`), canonicaljson.Limits{MaximumBytes: MaxJSONBytes, MaximumDepth: MaxJSONDepth})
	call, err := NewNamespacedToolCall(testItemID(t, 21), testCallID(t, 21), "clock", "curr_time", args)
	if err != nil {
		t.Fatal(err)
	}
	if call.Namespace() != "clock" || call.Name() != "curr_time" || !call.Valid() {
		t.Fatal("call identity mismatch")
	}
}

func TestRequestValidatesNamespaceDeclarationsCallsAndCapabilities(t *testing.T) {
	child := testNamespaceFunction(t, "curr_time")
	namespace, _ := NewToolNamespace("clock", "Time tools", []ToolDefinition{child})
	args, _ := canonicaljson.Parse([]byte(`{}`), canonicaljson.Limits{MaximumBytes: MaxJSONBytes, MaximumDepth: MaxJSONDepth})
	call, _ := NewNamespacedToolCall(testItemID(t, 22), testCallID(t, 22), "clock", "curr_time", args)
	item, _ := NewToolCallItem(call)
	choice, _ := NewNamespacedToolChoice("clock", "curr_time")
	request, err := NewRequest(RequestParams{ID: testRequestID(t, 22), Model: "model", Input: []Item{item}, Tools: []ToolDefinition{namespace}, ToolChoice: choice})
	if err != nil {
		t.Fatal(err)
	}
	if !request.Capabilities().Has(CapabilityTools) || !request.Capabilities().Has(CapabilityNamespaceTools) {
		t.Fatal("namespace capabilities missing")
	}
	encoded, err := EncodeRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(encoded)
	if err != nil {
		t.Fatal(err)
	}
	gotTool := decoded.Tools()[0]
	gotCall, _ := decoded.Input()[0].ToolCall()
	if gotTool.Kind() != ToolDefinitionKindNamespace || gotCall.Namespace() != "clock" || gotCall.Name() != "curr_time" || decoded.ToolChoice().Namespace() != "clock" {
		t.Fatalf("namespace round trip lost identity: %s", encoded)
	}
}

func TestNamespacedToolItemStartEventRoundTrip(t *testing.T) {
	event, err := NewItemStartedEvent(testEventID(t, 24), testResponseID(t, 24), 0, ItemStartParams{Index: 0, ID: testItemID(t, 24), Type: ItemType("tool_call"), CallID: testCallID(t, 24), Namespace: "clock", Name: "curr_time"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEvent(encoded)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := decoded.ItemStarted()
	if payload.Namespace() != "clock" || payload.Name() != "curr_time" {
		t.Fatalf("event namespace lost: %s", encoded)
	}
}

func TestToolNamespaceRejectsMalformedDefinitions(t *testing.T) {
	child := testNamespaceFunction(t, "child")
	for _, tc := range []struct {
		name        string
		description string
		tools       []ToolDefinition
	}{
		{"", "", []ToolDefinition{child}},
		{"bad name", "", []ToolDefinition{child}},
		{"ns", "", nil},
		{"ns", "", []ToolDefinition{child, child}},
	} {
		if value, err := NewToolNamespace(tc.name, tc.description, tc.tools); err == nil || value.Valid() {
			t.Fatalf("accepted malformed namespace %#v", tc)
		}
	}
}

func TestNamespaceFieldsRejectedOnNonToolEvents(t *testing.T) {
	if _, err := NewItemStartedEvent(testEventID(t, 25), testResponseID(t, 25), 0, ItemStartParams{Index: 0, ID: testItemID(t, 25), Type: ItemType("message"), Role: Role("assistant"), Namespace: "clock"}); err == nil {
		t.Fatal("message namespace accepted")
	}
}

func TestRequestRejectsNamespaceAggregateLimit(t *testing.T) {
	tools := make([]ToolDefinition, 0, 3)
	for namespaceIndex := 0; namespaceIndex < 3; namespaceIndex++ {
		children := make([]ToolDefinition, 0, 86)
		for childIndex := 0; childIndex < 86; childIndex++ {
			children = append(children, testNamespaceFunction(t, fmt.Sprintf("tool%d_%d", namespaceIndex, childIndex)))
		}
		namespace, err := NewToolNamespace(fmt.Sprintf("ns%d", namespaceIndex), "", children)
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, namespace)
	}
	args, _ := canonicaljson.Parse([]byte(`{}`), canonicaljson.DefaultLimits())
	call, _ := NewNamespacedToolCall(testItemID(t, 26), testCallID(t, 26), "ns0", "tool0_0", args)
	item, _ := NewToolCallItem(call)
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 26), Model: "model", Input: []Item{item}, Tools: tools}); err == nil {
		t.Fatal("aggregate namespace function limit accepted")
	}
}

func FuzzToolNamespace(f *testing.F) {
	f.Add("clock", "curr_time")
	f.Add("bad name", "tool")
	f.Fuzz(func(t *testing.T, namespaceName, childName string) {
		schema, err := canonicaljson.Parse([]byte(`{"type":"object"}`), canonicaljson.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		child, err := NewToolDefinition(childName, "", schema)
		if err != nil {
			return
		}
		namespace, err := NewToolNamespace(namespaceName, "", []ToolDefinition{child})
		if err == nil && !namespace.Valid() {
			t.Fatal("constructed invalid namespace")
		}
	})
}

func TestRequestRejectsUndeclaredNamespacedCall(t *testing.T) {
	child := testNamespaceFunction(t, "curr_time")
	namespace, _ := NewToolNamespace("clock", "", []ToolDefinition{child})
	args, _ := canonicaljson.Parse([]byte(`{}`), canonicaljson.Limits{MaximumBytes: MaxJSONBytes, MaximumDepth: MaxJSONDepth})
	call, _ := NewNamespacedToolCall(testItemID(t, 23), testCallID(t, 23), "other", "curr_time", args)
	item, _ := NewToolCallItem(call)
	if _, err := NewRequest(RequestParams{ID: testRequestID(t, 23), Model: "model", Input: []Item{item}, Tools: []ToolDefinition{namespace}}); err == nil {
		t.Fatal("undeclared namespaced call accepted")
	}
}
