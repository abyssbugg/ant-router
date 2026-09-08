package responses

import (
	"encoding/json"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// TestDecodeToolsRejectsNestedNamespace validates that nested namespaces are rejected.
func TestDecodeToolsRejectsNestedNamespace(t *testing.T) {
	raw := json.RawMessage(`[{
		"type": "namespace",
		"name": "outer",
		"description": "outer namespace",
		"tools": [{
			"type": "namespace",
			"name": "inner",
			"description": "inner namespace",
			"tools": [{
				"type": "function",
				"name": "func1",
				"parameters": {"type": "object"}
			}]
		}]
	}]`)
	_, err := decodeTools(raw)
	if err == nil {
		t.Fatal("nested namespace accepted")
	}
}

// TestDecodeToolsRejectsCustomFieldsInNamespace validates strict object validation.
func TestDecodeToolsRejectsCustomFieldsInNamespace(t *testing.T) {
	raw := json.RawMessage(`[{
		"type": "namespace",
		"name": "clock",
		"description": "time tools",
		"tools": [{
			"type": "function",
			"name": "curr_time",
			"parameters": {"type": "object"}
		}],
		"custom_field": "should_reject"
	}]`)
	_, err := decodeTools(raw)
	if err == nil {
		t.Fatal("custom field in namespace accepted")
	}
}

// TestDecodeToolsRejectsUnknownNamespaceFields validates strict object validation for namespace tools.
func TestDecodeToolsRejectsUnknownFieldsInNamespaceTool(t *testing.T) {
	raw := json.RawMessage(`[{
		"type": "namespace",
		"name": "clock",
		"description": "time tools",
		"tools": [{
			"type": "function",
			"name": "curr_time",
			"parameters": {"type": "object"},
			"unknown_field": "bad"
		}]
	}]`)
	_, err := decodeTools(raw)
	if err == nil {
		t.Fatal("unknown field in namespace tool accepted")
	}
}

// TestDecodeToolsAcceptsValidNamespace validates that well-formed namespaces are accepted.
func TestDecodeToolsAcceptsValidNamespace(t *testing.T) {
	raw := json.RawMessage(`[{
		"type": "namespace",
		"name": "clock",
		"description": "time tools",
		"tools": [{
			"type": "function",
			"name": "curr_time",
			"parameters": {"type": "object", "properties": {"tz": {"type": "string"}}}
		}]
	}]`)
	tools, err := decodeTools(raw)
	if err != nil {
		t.Fatalf("valid namespace rejected: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Kind() != canonical.ToolDefinitionKindNamespace {
		t.Fatalf("expected namespace kind, got %s", tools[0].Kind())
	}
	if tools[0].Name() != "clock" {
		t.Fatalf("expected name 'clock', got %s", tools[0].Name())
	}
	children := tools[0].Tools()
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0].Name() != "curr_time" {
		t.Fatalf("expected child name 'curr_time', got %s", children[0].Name())
	}
}

// TestDecodeToolsMixedFunctionAndNamespace validates that both function and namespace tools can coexist.
func TestDecodeToolsMixedFunctionAndNamespace(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type": "function",
			"name": "search",
			"description": "search documents",
			"parameters": {"type": "object"}
		},
		{
			"type": "namespace",
			"name": "clock",
			"description": "time tools",
			"tools": [{
				"type": "function",
				"name": "curr_time",
				"parameters": {"type": "object"}
			}]
		}
	]`)
	tools, err := decodeTools(raw)
	if err != nil {
		t.Fatalf("mixed tools rejected: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Kind() != canonical.ToolDefinitionKindFunction {
		t.Fatalf("expected first to be function, got %s", tools[0].Kind())
	}
	if tools[1].Kind() != canonical.ToolDefinitionKindNamespace {
		t.Fatalf("expected second to be namespace, got %s", tools[1].Kind())
	}
}

// TestDecodeToolsPreservesOrder validates that tool order is preserved.
func TestDecodeToolsPreservesOrder(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type": "namespace",
			"name": "ns1",
			"description": "namespace 1",
			"tools": [{
				"type": "function",
				"name": "func1",
				"parameters": {"type": "object"}
			}]
		},
		{
			"type": "namespace",
			"name": "ns2",
			"description": "namespace 2",
			"tools": [{
				"type": "function",
				"name": "func2",
				"parameters": {"type": "object"}
			}]
		}
	]`)
	tools, err := decodeTools(raw)
	if err != nil {
		t.Fatalf("valid tools rejected: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name() != "ns1" {
		t.Fatalf("expected first namespace 'ns1', got %s", tools[0].Name())
	}
	if tools[1].Name() != "ns2" {
		t.Fatalf("expected second namespace 'ns2', got %s", tools[1].Name())
	}
}

// TestDecodeToolsRejectsEmptyNamespaceTools validates that namespaces need at least one tool.
func TestDecodeToolsRejectsEmptyNamespaceTools(t *testing.T) {
	raw := json.RawMessage(`[{
		"type": "namespace",
		"name": "empty",
		"description": "empty namespace",
		"tools": []
	}]`)
	_, err := decodeTools(raw)
	if err == nil {
		t.Fatal("empty namespace tools accepted")
	}
}

// TestDecodeToolsRejectsDuplicateNamesInNamespace validates unique tool names within namespace.
func TestDecodeToolsRejectsDuplicateNamesInNamespace(t *testing.T) {
	raw := json.RawMessage(`[{
		"type": "namespace",
		"name": "clock",
		"description": "time tools",
		"tools": [{
			"type": "function",
			"name": "curr_time",
			"parameters": {"type": "object"}
		}, {
			"type": "function",
			"name": "curr_time",
			"parameters": {"type": "object"}
		}]
	}]`)
	_, err := decodeTools(raw)
	if err == nil {
		t.Fatal("duplicate names in namespace accepted")
	}
}

// TestDecodeToolsRejectsMissingNamespaceFields validates required fields.
func TestDecodeToolsRejectsMissingNamespaceFields(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
	}{
		{
			name: "missing tools",
			input: json.RawMessage(`[{
				"type": "namespace",
				"name": "clock",
				"description": "time tools"
			}]`),
		},
		{
			name: "missing name",
			input: json.RawMessage(`[{
				"type": "namespace",
				"description": "time tools",
				"tools": [{
					"type": "function",
					"name": "curr_time",
					"parameters": {"type": "object"}
				}]
			}]`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeTools(tc.input)
			if err == nil {
				t.Fatalf("missing field accepted in test %s", tc.name)
			}
		})
	}
}

// TestDecodeToolsAcceptsOptionalNamespaceDescription validates that description is optional.
func TestDecodeToolsAcceptsOptionalNamespaceDescription(t *testing.T) {
	raw := json.RawMessage(`[{
		"type": "namespace",
		"name": "clock",
		"tools": [{
			"type": "function",
			"name": "curr_time",
			"parameters": {"type": "object"}
		}]
	}]`)
	tools, err := decodeTools(raw)
	if err != nil {
		t.Fatalf("namespace without description rejected: %v", err)
	}
	if len(tools) != 1 || tools[0].Kind() != canonical.ToolDefinitionKindNamespace {
		t.Fatal("namespace not decoded properly")
	}
}

// TestCodexNamespaceRequestShape validates current Codex namespace request shape.
func TestCodexNamespaceRequestShape(t *testing.T) {
	raw := json.RawMessage(`[{
		"type": "namespace",
		"name": "clock",
		"description": "Time utilities",
		"tools": [{
			"type": "function",
			"name": "curr_time",
			"description": "Get current time",
			"parameters": {"type": "object"}
		}]
	}]`)
	tools, err := decodeTools(raw)
	if err != nil {
		t.Fatalf("namespace tool not decoded: %v", err)
	}
	if len(tools) != 1 || tools[0].Kind() != canonical.ToolDefinitionKindNamespace {
		t.Fatal("namespace tool not decoded properly")
	}
}
