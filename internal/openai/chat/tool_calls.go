package chat

import (
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
// was encoded from. Names absent from the map are plain function tools.
func resolveTool(nameMap ToolNameMap, upstream string) resolvedTool {
	if info, ok := nameMap[upstream]; ok {
		return resolvedTool{kind: info.Kind, namespace: info.Namespace, name: info.Name}
	}
	return resolvedTool{kind: canonical.ToolDefinitionKindFunction, name: upstream}
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
func decodeCustomInput(arguments string) (string, error) {
	value, err := canonicaljson.Parse([]byte(arguments), canonicaljson.DefaultLimits())
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
	parsed, err := canonicaljson.Parse([]byte(arguments), canonicaljson.DefaultLimits())
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
