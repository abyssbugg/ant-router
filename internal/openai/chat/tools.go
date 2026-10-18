package chat

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// ToolNameMap resolves the function names sent upstream back to the
// canonical tool each one stands for. Chat Completions has only plain
// function tools, so custom tools and namespace members are encoded as
// functions and must be mapped back when the model calls them.
type ToolNameMap map[string]ToolNameInfo

// ToolNameInfo identifies the canonical tool behind one upstream function.
type ToolNameInfo struct {
	// Kind is ToolDefinitionKindFunction or ToolDefinitionKindCustom; a
	// namespace member carries its own kind plus Namespace.
	Kind canonical.ToolDefinitionKind
	// Name is the canonical tool or namespace member name.
	Name string
	// Namespace is the owning namespace, empty for top-level tools.
	Namespace string
}

// namespaceSeparator joins a namespace and member name into one upstream
// function name.
const namespaceSeparator = "__"

// upstreamFunctionName returns the Chat Completions function name for a
// canonical tool name, flattening namespace members.
func upstreamFunctionName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + namespaceSeparator + name
}

// chatFunctionName is the function-name grammar Chat Completions accepts.
var chatFunctionName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// customToolParameters is the argument schema used for custom (freeform)
// tools: their free-text input travels as the single string field "input".
var customToolParameters = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"],"additionalProperties":false}`)

func encodeTools(tools []canonical.ToolDefinition) ([]map[string]any, error) {
	encoded, _, err := encodeToolsWithMap(tools)
	return encoded, err
}

// encodeToolsWithMap encodes canonical tool definitions as Chat Completions
// function tools and returns the mapping needed to decode calls to them.
func encodeToolsWithMap(tools []canonical.ToolDefinition) ([]map[string]any, ToolNameMap, error) {
	encoder := toolEncoder{names: make(ToolNameMap, len(tools))}
	for index, tool := range tools {
		path := fmt.Sprintf("request.tools[%d]", index)
		switch tool.Kind() {
		case canonical.ToolDefinitionKindFunction, canonical.ToolDefinitionKindCustom:
			if err := encoder.add(path, tool, "", ""); err != nil {
				return nil, nil, err
			}
		case canonical.ToolDefinitionKindNamespace:
			namespaceDesc := tool.Description()
			for memberIndex, member := range tool.Tools() {
				memberPath := fmt.Sprintf("%s.tools[%d]", path, memberIndex)
				if err := encoder.add(memberPath, member, tool.Name(), namespaceDesc); err != nil {
					return nil, nil, err
				}
			}
		default:
			return nil, nil, unsupported(path)
		}
	}
	return encoder.encoded, encoder.names, nil
}

type toolEncoder struct {
	encoded []map[string]any
	names   ToolNameMap
}

func (e *toolEncoder) add(path string, tool canonical.ToolDefinition, namespace, namespaceDesc string) error {
	name := upstreamFunctionName(namespace, tool.Name())
	if !chatFunctionName.MatchString(name) {
		return invalid(path+".name", fmt.Sprintf("function name %q must match %s", name, chatFunctionName))
	}
	if _, exists := e.names[name]; exists {
		return invalid(path+".name", fmt.Sprintf("function name %q is not unique", name))
	}
	// Include namespace description when available to provide context to the model
	description := tool.Description()
	if namespace != "" && namespaceDesc != "" {
		// Prepend namespace description to member description for context
		description = namespaceDesc + "\n\n" + tool.Description()
	}
	function := map[string]any{"name": name, "description": description}
	switch tool.Kind() {
	case canonical.ToolDefinitionKindFunction:
		function["parameters"] = json.RawMessage(tool.InputSchema().Bytes())
		if tool.Enforcement() == canonical.SchemaEnforcementStrict {
			function["strict"] = true
		}
	case canonical.ToolDefinitionKindCustom:
		function["parameters"] = customToolParameters
		if format := tool.CustomFormat(); format.Kind() == canonical.CustomToolFormatKindGrammar {
			function["description"] = fmt.Sprintf("%s\n\nThe input field must follow this %s grammar:\n%s",
				tool.Description(), format.Syntax(), format.Definition())
		}
	default:
		return unsupported(path)
	}
	e.encoded = append(e.encoded, map[string]any{"type": "function", "function": function})
	e.names[name] = ToolNameInfo{Kind: tool.Kind(), Name: tool.Name(), Namespace: namespace}
	return nil
}
