package chat

import (
	"strings"

	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

// resolvedTool is the canonical identity behind one upstream function name.
type resolvedTool struct {
	kind      canonical.ToolDefinitionKind
	namespace string
	name      string
}

// resolveTool maps an upstream function name back to the canonical tool it
// was encoded from. If a ToolNameMap is provided (non-nil and non-empty), all upstream names
// must be present; names absent from the map are rejected. An empty map is treated as nil.
func resolveTool(nameMap ToolNameMap, upstream string) (resolvedTool, error) {
	if info, ok := nameMap[upstream]; ok {
		return resolvedTool{kind: info.Kind, namespace: info.Namespace, name: info.Name}, nil
	}
	if len(nameMap) > 0 {
		// When a populated ToolNameMap exists but this name is missing, reject it
		return resolvedTool{}, protocolErr("tool.undeclared")
	}
	// When no ToolNameMap is provided (nil or empty), accept any upstream name as plain function
	return resolvedTool{kind: canonical.ToolDefinitionKindFunction, name: upstream}, nil
}

func (t resolvedTool) custom() bool { return t.kind == canonical.ToolDefinitionKindCustom }

func (t resolvedTool) itemType() canonical.ItemType {
	if t.custom() {
		return canonical.ItemType("custom_tool_call")
	}
	return canonical.ItemType("tool_call")
}

// decodeCustomInput extracts the freeform input of a custom tool call from its
// function arguments, which must be exactly the JSON object {"input": string}.
// Uses custom-input size limits (2 MiB) rather than the default JSON parsing limits.
func decodeCustomInput(arguments string) (string, error) {
	// Use custom limits that allow up to MaxRequestBytes (2 MiB) for custom inputs
	limits := canonicaljson.Limits{
		MaximumBytes: canonical.MaxRequestBytes,
		MaximumDepth: canonicaljson.DefaultLimits().MaximumDepth,
	}
	value, err := canonicaljson.Parse([]byte(arguments), limits)
	if err != nil || !value.IsObject() {
		return "", protocolErr("tool.custom_input")
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(value.Bytes(), &members); err != nil || len(members) != 1 {
		return "", protocolErr("tool.custom_input")
	}
	raw, ok := members["input"]
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return "", protocolErr("tool.custom_input")
	}
	var input string
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", protocolErr("tool.custom_input")
	}
	// The input size bound is enforced by canonical.NewCustomToolCall; the
	// buffered arguments are already capped at canonical.MaxJSONBytes.
	return input, nil
}

// buildToolCallItem builds the completed canonical item for one model tool
// call: a custom tool call for custom tools, otherwise a (possibly
// namespaced) function tool call.
func buildToolCallItem(tool resolvedTool, itemID canonical.ItemID, callID canonical.ToolCallID, arguments string) (canonical.Item, error) {
	if tool.custom() {
		input, err := decodeCustomInput(arguments)
		if err != nil {
			return canonical.Item{}, err
		}
		var call canonical.CustomToolCall
		if tool.namespace == "" {
			call, err = canonical.NewCustomToolCall(itemID, callID, tool.name, input)
		} else {
			call, err = canonical.NewNamespacedCustomToolCall(itemID, callID, tool.namespace, tool.name, input)
		}
		if err != nil {
			return canonical.Item{}, protocolErr("tool.item")
		}
		item, err := canonical.NewCustomToolCallItem(call)
		if err != nil {
			return canonical.Item{}, protocolErr("tool.item")
		}
		return item, nil
	}
	// Providers stream parameterless function calls with no argument
	// fragments; an empty (whitespace-only) argument string is the empty
	// object. Custom tools keep their raw input above.
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	// Use custom limits that allow up to MaxRequestBytes (2 MiB) for tool arguments
	limits := canonicaljson.Limits{
		MaximumBytes: canonical.MaxRequestBytes,
		MaximumDepth: canonicaljson.DefaultLimits().MaximumDepth,
	}
	parsed, err := canonicaljson.Parse([]byte(arguments), limits)
	if err != nil {
		return canonical.Item{}, protocolErr("tool.arguments")
	}
	var call canonical.ToolCall
	if tool.namespace == "" {
		call, err = canonical.NewToolCall(itemID, callID, tool.name, parsed)
	} else {
		call, err = canonical.NewNamespacedToolCall(itemID, callID, tool.namespace, tool.name, parsed)
	}
	if err != nil {
		return canonical.Item{}, protocolErr("tool.item")
	}
	item, err := canonical.NewToolCallItem(call)
	if err != nil {
		return canonical.Item{}, protocolErr("tool.item")
	}
	return item, nil
}
