package canonical

import "unicode/utf8"

// CustomToolCall is an immutable canonical freeform tool invocation.
type CustomToolCall struct {
	id        ItemID
	callID    ToolCallID
	namespace string
	name      string
	input     string
}

// NewCustomToolCall validates a direct canonical custom-tool invocation.
func NewCustomToolCall(id ItemID, callID ToolCallID, name, input string) (CustomToolCall, error) {
	return newCustomToolCall(id, callID, "", name, input)
}

// NewNamespacedCustomToolCall validates a namespaced canonical custom-tool invocation.
func NewNamespacedCustomToolCall(id ItemID, callID ToolCallID, namespace, name, input string) (CustomToolCall, error) {
	if !validToolName(namespace) {
		return CustomToolCall{}, itemError(CodeInvalidInvariant, "custom_tool_call.namespace")
	}
	return newCustomToolCall(id, callID, namespace, name, input)
}

func newCustomToolCall(id ItemID, callID ToolCallID, namespace, name, input string) (CustomToolCall, error) {
	if !id.Valid() {
		return CustomToolCall{}, itemError(CodeInvalidID, "custom_tool_call.id")
	}
	if !callID.Valid() {
		return CustomToolCall{}, itemError(CodeInvalidID, "custom_tool_call.call_id")
	}
	if !validToolName(name) {
		return CustomToolCall{}, itemError(CodeInvalidInvariant, "custom_tool_call.name")
	}
	if !utf8.ValidString(input) {
		return CustomToolCall{}, itemError(CodeInvalidUTF8, "custom_tool_call.input")
	}
	if len(input) > MaxCustomToolInputBytes {
		return CustomToolCall{}, itemError(CodeLimitExceeded, "custom_tool_call.input")
	}
	if !validCustomTextCharacters(input) {
		return CustomToolCall{}, itemError(CodeInvalidInvariant, "custom_tool_call.input")
	}
	return CustomToolCall{id: id, callID: callID, namespace: namespace, name: name, input: input}, nil
}

func (c CustomToolCall) ID() ItemID         { return c.id }
func (c CustomToolCall) CallID() ToolCallID { return c.callID }
func (c CustomToolCall) Namespace() string  { return c.namespace }
func (c CustomToolCall) Name() string       { return c.name }
func (c CustomToolCall) Input() string      { return c.input }
func (c CustomToolCall) Valid() bool {
	return c.id.Valid() && c.callID.Valid() && (c.namespace == "" || validToolName(c.namespace)) && validToolName(c.name) && utf8.ValidString(c.input) && len(c.input) <= MaxCustomToolInputBytes && validCustomTextCharacters(c.input)
}
