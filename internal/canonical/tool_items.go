package canonical

import "github.com/abyssbugg/ant-router/internal/canonicaljson"

// ToolCall is an immutable canonical tool invocation.
type ToolCall struct {
	id        ItemID
	callID    ToolCallID
	name      string
	arguments canonicaljson.Value
}

// NewToolCall validates a canonical tool invocation.
func NewToolCall(id ItemID, callID ToolCallID, name string, arguments canonicaljson.Value) (ToolCall, error) {
	if !id.Valid() {
		return ToolCall{}, itemError(CodeInvalidID, "tool_call.id")
	}
	if !callID.Valid() {
		return ToolCall{}, itemError(CodeInvalidID, "tool_call.call_id")
	}
	if !validToolName(name) {
		return ToolCall{}, itemError(CodeInvalidInvariant, "tool_call.name")
	}
	if !arguments.IsObject() {
		return ToolCall{}, itemError(CodeInvalidJSON, "tool_call.arguments")
	}
	if len(arguments.Bytes()) > MaxJSONBytes {
		return ToolCall{}, itemError(CodeLimitExceeded, "tool_call.arguments")
	}
	return ToolCall{id: id, callID: callID, name: name, arguments: arguments}, nil
}

func (c ToolCall) ID() ItemID                     { return c.id }
func (c ToolCall) CallID() ToolCallID             { return c.callID }
func (c ToolCall) Name() string                   { return c.name }
func (c ToolCall) Arguments() canonicaljson.Value { return c.arguments }
func (c ToolCall) Valid() bool {
	return c.id.Valid() && c.callID.Valid() && validToolName(c.name) && c.arguments.IsObject() && len(c.arguments.Bytes()) <= MaxJSONBytes
}

// ToolResult is an immutable canonical result for a prior call.
type ToolResult struct {
	id      ItemID
	callID  ToolCallID
	content []ContentBlock
	isError bool
}

// NewToolResult validates and copies a canonical tool result.
func NewToolResult(id ItemID, callID ToolCallID, content []ContentBlock, isError bool) (ToolResult, error) {
	if !id.Valid() {
		return ToolResult{}, itemError(CodeInvalidID, "tool_result.id")
	}
	if !callID.Valid() {
		return ToolResult{}, itemError(CodeInvalidID, "tool_result.call_id")
	}
	if err := validateContentBlocks(content, "tool_result.content"); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{id: id, callID: callID, content: append([]ContentBlock(nil), content...), isError: isError}, nil
}

func (r ToolResult) ID() ItemID              { return r.id }
func (r ToolResult) CallID() ToolCallID      { return r.callID }
func (r ToolResult) Content() []ContentBlock { return append([]ContentBlock(nil), r.content...) }
func (r ToolResult) IsError() bool           { return r.isError }
func (r ToolResult) Valid() bool {
	return r.id.Valid() && r.callID.Valid() && validateContentBlocks(r.content, "tool_result.content") == nil
}
