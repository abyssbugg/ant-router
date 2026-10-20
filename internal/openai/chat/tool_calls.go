package chat

import (
	"encoding/json"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

// MaxCustomArgumentBytes is the wire size limit for custom-tool arguments.
// Custom inputs are up to 2 MiB decoded; when JSON-encoded with {"input":"..."},
// each byte can escape to up to 6 bytes (\uXXXX), plus the wrapper overhead.
const MaxCustomArgumentBytes = 6*canonical.MaxCustomToolInputBytes + len(`{"input":""}`)

// resolvedTool is the canonical identity behind one upstream function name.
type resolvedTool struct {
	kind      canonical.ToolDefinitionKind
	namespace string
	name      string
}

// resolveTool maps an upstream function name back to the canonical tool it
// was encoded from. A nil map means the caller declared no name mapping and
// every name is a plain function tool. A non-nil map, even an empty one, is
// the complete set of tools the request declared, so any other name is an
// undeclared call that must not enter the canonical history.
func resolveTool(nameMap ToolNameMap, upstream string) (resolvedTool, error) {
	if nameMap == nil {
		return resolvedTool{kind: canonical.ToolDefinitionKindFunction, name: upstream}, nil
	}
	info, ok := nameMap[upstream]
	if !ok {
		return resolvedTool{}, protocolErr("tool.undeclared")
	}
	return resolvedTool{kind: info.Kind, namespace: info.Namespace, name: info.Name}, nil
}

// declaresCustomTool reports whether the map contains any custom tool.
func (m ToolNameMap) declaresCustomTool() bool {
	for _, info := range m {
		if info.Kind == canonical.ToolDefinitionKindCustom {
			return true
		}
	}
	return false
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
// Parses with custom-input size limits to accommodate the 2 MiB decoded limit
// plus JSON escaping overhead.
func decodeCustomInput(arguments string) (string, error) {
	limits := canonicaljson.Limits{
		MaximumBytes: MaxCustomArgumentBytes,
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
	// buffered arguments are already capped at maxCustomArgumentBytes.
	if len(input) > canonical.MaxCustomToolInputBytes {
		return "", protocolErr("tool.custom_input")
	}
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
	if strings.Trim(arguments, " \t\r\n") == "" {
		arguments = "{}"
	}
	// Function tool arguments are limited to MaxJSONBytes (1 MiB)
	limits := canonicaljson.Limits{
		MaximumBytes: canonical.MaxJSONBytes,
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
