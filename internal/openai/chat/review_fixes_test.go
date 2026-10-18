package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func assertChatError(t *testing.T, err error, code, path string) {
	t.Helper()
	var target *Error
	if !errors.As(err, &target) {
		t.Fatalf("error = %v, want *Error %s at %s", err, code, path)
	}
	if target.Code != code || target.Path != path {
		t.Fatalf("error = %s at %s, want %s at %s", target.Code, target.Path, code, path)
	}
}

func buildToolResponse(t *testing.T, nameMap ToolNameMap, name, arguments string) (canonical.Response, error) {
	t.Helper()
	calls := []map[string]any{{"id": "c0", "type": "function", "function": map[string]any{"name": name, "arguments": arguments}}}
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
	return BuildResponse(responseID, "public/model", completion, ids, nameMap)
}

func toolCallChunk(t *testing.T, index int, id, name, arguments string) string {
	t.Helper()
	function := map[string]any{"arguments": arguments}
	if name != "" {
		function["name"] = name
	}
	call := map[string]any{"index": index, "function": function}
	if id != "" {
		call["id"] = id
	}
	encoded, err := json.Marshal(map[string]any{"id": "c1", "model": "m", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

const finishToolCallsChunk = `{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`

func TestUndeclaredToolCallsAreRejected(t *testing.T) {
	for name, nameMap := range map[string]ToolNameMap{"populated": mappingNameMap(), "empty": {}} {
		t.Run(name+"/response", func(t *testing.T) {
			_, err := buildToolResponse(t, nameMap, "ghost", `{}`)
			assertChatError(t, err, "protocol", "tool.undeclared")
		})
		t.Run(name+"/stream", func(t *testing.T) {
			_, _, err := runMappingStream(t, []string{toolCallChunk(t, 0, "call_0", "ghost", `{}`), finishToolCallsChunk}, nameMap)
			assertChatError(t, err, "protocol", "tool.undeclared")
		})
	}
}

func TestFunctionArgumentsAcceptOnlyJSONWhitespaceAsEmpty(t *testing.T) {
	nameMap := mappingNameMap()
	t.Run("response json whitespace", func(t *testing.T) {
		response, err := buildToolResponse(t, nameMap, "lookup", " \n\t\r")
		if err != nil {
			t.Fatal(err)
		}
		call, _ := response.Items()[0].ToolCall()
		if got := string(call.Arguments().Bytes()); got != `{}` {
			t.Fatalf("arguments = %s, want {}", got)
		}
	})
	t.Run("response nbsp", func(t *testing.T) {
		_, err := buildToolResponse(t, nameMap, "lookup", "\u00a0")
		assertChatError(t, err, "protocol", "tool.arguments")
	})
	t.Run("stream nbsp", func(t *testing.T) {
		_, _, err := runMappingStream(t, []string{toolCallChunk(t, 0, "call_0", "lookup", "\u00a0"), finishToolCallsChunk}, nameMap)
		if err == nil {
			t.Fatal("stream accepted non-JSON whitespace as function arguments")
		}
	})
}

// streamCustomInput streams one apply_patch call whose input is split into
// fragments of the encoded arguments.
func streamCustomInput(t *testing.T, input string) error {
	t.Helper()
	arguments := customArguments(t, input)
	chunks := []string{toolCallChunk(t, 0, "call_0", "apply_patch", "")}
	const fragment = 256 << 10
	for start := 0; start < len(arguments); start += fragment {
		end := min(start+fragment, len(arguments))
		chunks = append(chunks, toolCallChunk(t, 0, "", "", arguments[start:end]))
	}
	chunks = append(chunks, finishToolCallsChunk)
	_, _, err := runMappingStream(t, chunks, mappingNameMap())
	return err
}

func TestCustomInputsBeyondTheJSONLimitRoundTrip(t *testing.T) {
	// Each input is valid canonically but its encoded arguments exceed
	// canonical.MaxJSONBytes, the bound previously (wrongly) applied to
	// custom calls. Quotes double in the JSON wrapper.
	inputs := map[string]string{
		"plain":   strings.Repeat("a", canonical.MaxCustomToolInputBytes-(64<<10)),
		"escaped": strings.Repeat(`"`, 600<<10),
	}
	for name, input := range inputs {
		if len(customArguments(t, input)) <= canonical.MaxJSONBytes {
			t.Fatalf("%s fixture does not exceed MaxJSONBytes", name)
		}
		t.Run(name+"/stream", func(t *testing.T) {
			if err := streamCustomInput(t, input); err != nil {
				t.Fatalf("valid custom input rejected: %v", err)
			}
		})
		t.Run(name+"/response", func(t *testing.T) {
			response, err := buildToolResponse(t, mappingNameMap(), "apply_patch", customArguments(t, input))
			if err != nil {
				t.Fatalf("valid custom input rejected: %v", err)
			}
			call, _ := response.Items()[0].CustomToolCall()
			if call.Input() != input {
				t.Fatal("custom input did not round-trip")
			}
		})
	}
	t.Run("stream over limit", func(t *testing.T) {
		err := streamCustomInput(t, strings.Repeat("a", canonical.MaxCustomToolInputBytes+1))
		assertChatError(t, err, "protocol", "tool.custom_input")
	})
	t.Run("response over limit", func(t *testing.T) {
		_, err := buildToolResponse(t, mappingNameMap(), "apply_patch", customArguments(t, strings.Repeat("a", canonical.MaxCustomToolInputBytes+1)))
		assertChatError(t, err, "protocol", "tool.custom_input")
	})
}

func TestFunctionArgumentsStayBoundedByJSONLimit(t *testing.T) {
	big := `{"v":"` + strings.Repeat("a", canonical.MaxJSONBytes) + `"}`
	_, err := buildToolResponse(t, mappingNameMap(), "lookup", big)
	assertChatError(t, err, "protocol", "tool.arguments")
}

func TestPendingArgumentsUseCustomBoundOnlyWhenCustomToolDeclared(t *testing.T) {
	ids := &countingIDs{}
	responseID, err := ids.ResponseID()
	if err != nil {
		t.Fatal(err)
	}
	accumulator := NewAccumulator(responseID, "public/model", ids, mappingNameMap())
	fragment := strings.Repeat("a", 64<<10)
	chunk, err := DecodeChunk([]byte(toolCallChunk(t, 0, "", "", fragment)))
	if err != nil {
		t.Fatal(err)
	}
	for sent := 0; sent <= canonical.MaxJSONBytes+len(fragment); sent += len(fragment) {
		if _, err := accumulator.Accept(chunk); err != nil {
			t.Fatalf("pending arguments rejected after %d bytes with a custom tool declared: %v", sent, err)
		}
	}
}

func TestIsInvalidClassifiesEncodingErrors(t *testing.T) {
	if !IsInvalid(invalid("request.tools[0].name", "bad")) {
		t.Fatal("invalid error not classified as invalid")
	}
	for _, err := range []error{protocolErr("tool.item"), unsupported("request.input[0]"), errors.New("plain")} {
		if IsInvalid(err) {
			t.Fatalf("%v classified as invalid", err)
		}
	}
}

func strictTool(t *testing.T, schema string) canonical.ToolDefinition {
	t.Helper()
	value, err := canonicaljson.Parse([]byte(schema), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	tool, err := canonical.NewToolDefinitionWithEnforcement("exact", "Exact", value, canonical.SchemaEnforcementStrict)
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestStrictToolSchemasMustSatisfyStrictMode(t *testing.T) {
	compliant := `{"type":"object","additionalProperties":false,"required":["a","list"],"properties":{` +
		`"a":{"type":"object","additionalProperties":false,"required":["b"],"properties":{"b":{"type":"string"}}},` +
		`"list":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["c"],"properties":{"c":{"type":"integer"}}}}}}`
	if _, err := encodeTools([]canonical.ToolDefinition{strictTool(t, compliant)}); err != nil {
		t.Fatalf("compliant strict schema rejected: %v", err)
	}
	cases := map[string]string{
		"missing additionalProperties": `{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}}`,
		"additionalProperties true":    `{"type":"object","additionalProperties":true,"required":["a"],"properties":{"a":{"type":"string"}}}`,
		"missing required key":         `{"type":"object","additionalProperties":false,"required":["a"],"properties":{"a":{"type":"string"},"b":{"type":"string"}}}`,
		"nested object":                `{"type":"object","additionalProperties":false,"required":["a"],"properties":{"a":{"type":"object","properties":{"b":{"type":"string"}},"required":["b"]}}}`,
		"array items object":           `{"type":"object","additionalProperties":false,"required":["l"],"properties":{"l":{"type":"array","items":{"type":"object","properties":{},"additionalProperties":true}}}}`,
		"anyOf branch":                 `{"type":"object","additionalProperties":false,"required":["u"],"properties":{"u":{"anyOf":[{"type":"string"},{"type":"object","properties":{"x":{"type":"string"}}}]}}}`,
	}
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := encodeTools([]canonical.ToolDefinition{strictTool(t, schema)})
			var target *Error
			if !errors.As(err, &target) || target.Code != "invalid" || !strings.HasPrefix(target.Path, "request.tools[0].parameters") {
				t.Fatalf("error = %v, want invalid at request.tools[0].parameters", err)
			}
		})
	}
	// Non-strict tools are forwarded unchanged regardless of shape.
	loose, err := canonicaljson.Parse([]byte(`{"type":"object"}`), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	tool, err := canonical.NewToolDefinition("loose", "Loose", loose)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encodeTools([]canonical.ToolDefinition{tool}); err != nil {
		t.Fatalf("non-strict tool rejected: %v", err)
	}
}

func TestFlattenedNamespaceMembersCarryNamespaceDescription(t *testing.T) {
	ns := mustNamespace(t, "mcp", "Tools served by the MCP server", []canonical.ToolDefinition{
		mustFunctionTool(t, "get", "Get a record"),
		mustCustomGrammarTool(t, "patch", "Apply a patch", canonical.CustomGrammarSyntax("lark"), `start: "x"`),
	})
	encoded, err := encodeTools([]canonical.ToolDefinition{ns})
	if err != nil {
		t.Fatal(err)
	}
	for name, member := range map[string]string{"mcp__get": "Get a record", "mcp__patch": "Apply a patch"} {
		description, _ := encodedFunction(t, encoded, name)["description"].(string)
		if !strings.Contains(description, "Tools served by the MCP server") || !strings.Contains(description, member) {
			t.Fatalf("%s description = %q, want namespace and member guidance", name, description)
		}
	}
}

func TestEncodedRequestBodyIsBounded(t *testing.T) {
	// Each quote escapes twice: once in the arguments string and again in
	// the request body, so a history within canonical limits can expand
	// past canonical.MaxRequestBytes on the chat wire.
	input := strings.Repeat(`"`, canonical.MaxCustomToolInputBytes)
	ids := &countingIDs{}
	var items []canonical.Item
	for index := 0; index < 5; index++ {
		itemID, err := ids.ItemID()
		if err != nil {
			t.Fatal(err)
		}
		callID, err := ids.ToolCallID()
		if err != nil {
			t.Fatal(err)
		}
		call, err := canonical.NewCustomToolCall(itemID, callID, "apply_patch", input)
		if err != nil {
			t.Fatal(err)
		}
		item, err := canonical.NewCustomToolCallItem(call)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    fixedRequestID(t, 1),
		Model: "deepseek-chat",
		Input: items,
		Tools: []canonical.ToolDefinition{mustCustomTextTool(t, "apply_patch", "Apply a patch")},
	})
	if err != nil {
		t.Skipf("canonical request rejected the fixture before encoding: %v", err)
	}
	_, _, err = EncodeRequestWithTools(request, "deepseek-chat", true)
	assertChatError(t, err, "invalid", "request")
}
