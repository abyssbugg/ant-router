package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func mustCustomTextTool(t *testing.T, name, description string) canonical.ToolDefinition {
	t.Helper()
	tool, err := canonical.NewCustomTool(name, description)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func mustCustomGrammarTool(t *testing.T, name, description string, syntax canonical.CustomGrammarSyntax, definition string) canonical.ToolDefinition {
	t.Helper()
	format, err := canonical.NewCustomGrammarFormat(syntax, definition)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := canonical.NewCustomToolWithFormat(name, description, format)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func mustJSONSchema(t *testing.T) canonicaljson.Value {
	t.Helper()
	schema := `{"type":"object","properties":{"arg":{"type":"string"}},"required":["arg"],"additionalProperties":false}`
	val, err := canonicaljson.Parse([]byte(schema), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return val
}

func mustFunctionTool(t *testing.T, name, description string) canonical.ToolDefinition {
	t.Helper()
	tool, err := canonical.NewToolDefinition(name, description, mustJSONSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func mustNamespace(t *testing.T, name, description string, members []canonical.ToolDefinition) canonical.ToolDefinition {
	t.Helper()
	ns, err := canonical.NewToolNamespace(name, description, members)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

// TestEncodeToolsCustomTextNoGrammar: custom tool without grammar becomes function with string input schema
func TestEncodeToolsCustomTextNoGrammar(t *testing.T) {
	tool := mustCustomTextTool(t, "search_docs", "Search internal documentation")
	encoded, err := encodeTools([]canonical.ToolDefinition{tool})
	if err != nil {
		t.Fatalf("encodeTools failed: %v", err)
	}
	if len(encoded) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(encoded))
	}
	toolObj := encoded[0]
	if toolObj["type"] != "function" {
		t.Errorf("expected type=function, got %v", toolObj["type"])
	}
	funcObj := toolObj["function"].(map[string]any)
	if funcObj["name"] != "search_docs" {
		t.Errorf("expected name=search_docs, got %v", funcObj["name"])
	}
	if funcObj["description"] != "Search internal documentation" {
		t.Errorf("expected correct description")
	}
	want := `{"additionalProperties":false,"properties":{"input":{"type":"string"}},"required":["input"],"type":"object"}`
	var decoded any
	if err := json.Unmarshal([]byte(encodedParameters(t, funcObj)), &decoded); err != nil {
		t.Fatal(err)
	}
	canonicalJSON, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonicalJSON) != want {
		t.Errorf("custom tool parameters = %s, want %s", canonicalJSON, want)
	}
}

// TestEncodeToolsCustomWithGrammar: custom tool with grammar includes grammar note in description
func TestEncodeToolsCustomWithGrammar(t *testing.T) {
	larkDef := `start: "hello" name\nname: /\\w+/`
	tool := mustCustomGrammarTool(t, "greet", "Greet by name", canonical.CustomGrammarSyntaxLark, larkDef)
	encoded, err := encodeTools([]canonical.ToolDefinition{tool})
	if err != nil {
		t.Fatalf("encodeTools failed: %v", err)
	}
	if len(encoded) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(encoded))
	}
	funcObj := encoded[0]["function"].(map[string]any)
	desc := funcObj["description"].(string)
	if !strings.Contains(desc, "Greet by name") {
		t.Errorf("expected original description in output")
	}
	if !strings.Contains(desc, "lark") || !strings.Contains(desc, larkDef) {
		t.Errorf("expected grammar note with syntax and definition in description")
	}
}

// TestEncodeToolsNamespaceFlatten: namespace with 2 members becomes 2 flattened functions
func TestEncodeToolsNamespaceFlatten(t *testing.T) {
	fn1 := mustFunctionTool(t, "get_user", "Get user by ID")
	fn2 := mustFunctionTool(t, "list_users", "List all users")
	ns := mustNamespace(t, "users", "User operations", []canonical.ToolDefinition{fn1, fn2})

	encoded, err := encodeTools([]canonical.ToolDefinition{ns})
	if err != nil {
		t.Fatalf("encodeTools failed: %v", err)
	}
	if len(encoded) != 2 {
		t.Fatalf("expected 2 flattened tools, got %d", len(encoded))
	}

	names := map[string]bool{}
	for _, toolObj := range encoded {
		if toolObj["type"] != "function" {
			t.Errorf("expected type=function")
		}
		funcObj := toolObj["function"].(map[string]any)
		name := funcObj["name"].(string)
		names[name] = true
		if !strings.HasPrefix(name, "users__") {
			t.Errorf("expected name to start with namespace prefix")
		}
	}
	if !names["users__get_user"] || !names["users__list_users"] {
		t.Errorf("expected flattened names users__get_user and users__list_users")
	}
}

// TestEncodeToolsNameTooLong: function name > 64 chars returns error with exact path and code
func TestEncodeToolsNameTooLong(t *testing.T) {
	fn1 := mustFunctionTool(t, "get", "Get")
	// 60 + len("__") + 3 == 65: rejected with exact path.
	ns := mustNamespace(t, strings.Repeat("n", 60), "desc", []canonical.ToolDefinition{fn1})

	_, err := encodeTools([]canonical.ToolDefinition{ns})
	var target *Error
	if !errors.As(err, &target) || target.Code != "invalid" || target.Path != "request.tools[0].tools[0].name" {
		t.Fatalf("err = %#v, want invalid at request.tools[0].tools[0].name", err)
	}

	// 59 + len("__") + 3 == 64: accepted (boundary case).
	fnOk := mustFunctionTool(t, "get", "Get")
	nsOk := mustNamespace(t, strings.Repeat("n", 59), "desc", []canonical.ToolDefinition{fnOk})
	_, err = encodeTools([]canonical.ToolDefinition{nsOk})
	if err != nil {
		t.Fatalf("64-byte flattened name rejected: %v", err)
	}
}

// TestEncodeToolsCollisionCustomFunctionNamespace: collision between flattened and direct names
func TestEncodeToolsCollisionCustomFunctionNamespace(t *testing.T) {
	// Create a direct function "x__foo" and a namespace "x" with member "foo"
	// When flattened, namespace member becomes "x__foo" which collides with direct function
	fn1 := mustFunctionTool(t, "x__foo", "Direct")
	fn2 := mustFunctionTool(t, "foo", "Member")
	ns := mustNamespace(t, "x", "desc", []canonical.ToolDefinition{fn2})

	_, err := encodeTools([]canonical.ToolDefinition{fn1, ns})
	var target *Error
	if !errors.As(err, &target) || target.Code != "invalid" || target.Path != "request.tools[1].tools[0].name" {
		t.Fatalf("err = %#v, want invalid at request.tools[1].tools[0].name", err)
	}
}

// TestToolNameMapPopulation: ToolNameMap is correctly built during encoding
func TestToolNameMapPopulation(t *testing.T) {
	fn1 := mustFunctionTool(t, "calc", "Calculate")
	custom := mustCustomTextTool(t, "reason", "Reason about")
	fn2 := mustFunctionTool(t, "user_get", "Get user")
	fn3 := mustFunctionTool(t, "user_list", "List users")
	ns := mustNamespace(t, "user", "User ops", []canonical.ToolDefinition{fn2, fn3})

	toolChoice, err := canonical.NewToolChoice(canonical.ToolChoiceKind("auto"), "")
	if err != nil {
		t.Fatal(err)
	}
	output, err := canonical.NewOutputConstraints(canonical.OutputParams{Format: canonical.OutputFormat("text")})
	if err != nil {
		t.Fatal(err)
	}
	// Create a minimal user message
	text, _ := canonical.NewTextContent("test")
	userBlock, _ := canonical.NewTextBlock(text)
	userMessage, _ := canonical.NewMessage(fixedItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{userBlock})
	userItem, _ := canonical.NewMessageItem(userMessage)

	req, err := canonical.NewRequest(canonical.RequestParams{
		ID:         fixedRequestID(t, 1),
		Model:      "gpt-4o",
		Input:      []canonical.Item{userItem},
		Tools:      []canonical.ToolDefinition{fn1, custom, ns},
		ToolChoice: toolChoice,
		Output:     output,
	})
	if err != nil {
		t.Fatal(err)
	}

	body, nameMap, err := EncodeRequestWithTools(req, "gpt-4o", true)
	if err != nil {
		t.Fatalf("EncodeRequestWithTools failed: %v", err)
	}
	if body == nil || len(body) == 0 {
		t.Errorf("expected body")
	}

	// Check nameMap contents
	if nameMap == nil {
		t.Fatalf("expected ToolNameMap to be populated")
	}
	want := ToolNameMap{
		"calc":            {Kind: canonical.ToolDefinitionKindFunction, Name: "calc"},
		"reason":          {Kind: canonical.ToolDefinitionKindCustom, Name: "reason"},
		"user__user_get":  {Kind: canonical.ToolDefinitionKindFunction, Name: "user_get", Namespace: "user"},
		"user__user_list": {Kind: canonical.ToolDefinitionKindFunction, Name: "user_list", Namespace: "user"},
	}
	if len(nameMap) != len(want) {
		t.Fatalf("nameMap = %#v, want %#v", nameMap, want)
	}
	for name, info := range want {
		if nameMap[name] != info {
			t.Errorf("nameMap[%q] = %#v, want %#v", name, nameMap[name], info)
		}
	}
}

// TestExistingTestsStillPass: ensure backward compatibility
func TestExistingEncodeToolsFunctionOnly(t *testing.T) {
	fn := mustFunctionTool(t, "old_tool", "An old function tool")
	encoded, err := encodeTools([]canonical.ToolDefinition{fn})
	if err != nil {
		t.Fatalf("encodeTools failed for function: %v", err)
	}
	if len(encoded) != 1 {
		t.Fatalf("expected 1 tool")
	}
	if encoded[0]["type"] != "function" {
		t.Errorf("expected function type")
	}
}

func encodedFunction(t *testing.T, encoded []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range encoded {
		function := tool["function"].(map[string]any)
		if function["name"] == name {
			return function
		}
	}
	t.Fatalf("function %q not encoded", name)
	return nil
}

func encodedParameters(t *testing.T, function map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(function["parameters"])
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestEncodeToolsPreservesFunctionSchemaAndStrictness(t *testing.T) {
	strict, err := canonical.NewToolDefinitionWithEnforcement("exact", "Exact", mustJSONSchema(t), canonical.SchemaEnforcementStrict)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeTools([]canonical.ToolDefinition{mustFunctionTool(t, "loose", "Loose"), strict})
	if err != nil {
		t.Fatal(err)
	}
	schema := string(mustJSONSchema(t).Bytes())
	for _, name := range []string{"loose", "exact"} {
		if got := encodedParameters(t, encodedFunction(t, encoded, name)); got != schema {
			t.Errorf("%s parameters = %s, want declared schema %s", name, got, schema)
		}
	}
	if encodedFunction(t, encoded, "exact")["strict"] != true {
		t.Error("strict function lost strict enforcement")
	}
	if _, present := encodedFunction(t, encoded, "loose")["strict"]; present {
		t.Error("non-strict function unexpectedly marked strict")
	}
}

func TestEncodeToolsPreservesNamespaceMemberSchemaAndCustomKind(t *testing.T) {
	strict, err := canonical.NewToolDefinitionWithEnforcement("get", "Get", mustJSONSchema(t), canonical.SchemaEnforcementStrict)
	if err != nil {
		t.Fatal(err)
	}
	ns := mustNamespace(t, "mcp", "MCP server", []canonical.ToolDefinition{strict, mustCustomTextTool(t, "patch", "Patch")})
	encoded, nameMap, err := encodeToolsWithMap([]canonical.ToolDefinition{ns})
	if err != nil {
		t.Fatal(err)
	}
	member := encodedFunction(t, encoded, "mcp__get")
	if got, want := encodedParameters(t, member), string(mustJSONSchema(t).Bytes()); got != want {
		t.Errorf("namespace member parameters = %s, want %s", got, want)
	}
	if member["strict"] != true {
		t.Error("strict namespace member lost strict enforcement")
	}
	if got := encodedParameters(t, encodedFunction(t, encoded, "mcp__patch")); !strings.Contains(got, `"input"`) {
		t.Errorf("custom namespace member parameters = %s, want input wrapper", got)
	}
	if got, want := nameMap["mcp__patch"], (ToolNameInfo{Kind: canonical.ToolDefinitionKindCustom, Name: "patch", Namespace: "mcp"}); got != want {
		t.Errorf("nameMap[mcp__patch] = %#v, want %#v", got, want)
	}
}
