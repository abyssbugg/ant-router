package canonical

// Item is an immutable discriminated canonical item.
type Item struct {
	kind       ItemType
	message    Message
	toolCall   ToolCall
	customCall CustomToolCall
	toolResult ToolResult
	reasoning  ReasoningSummary
}

func NewMessageItem(value Message) (Item, error) {
	if !value.Valid() {
		return Item{}, itemError(CodeInvalidInvariant, "item")
	}
	return Item{kind: ItemType("message"), message: value}, nil
}
func NewToolCallItem(value ToolCall) (Item, error) {
	if !value.Valid() {
		return Item{}, itemError(CodeInvalidInvariant, "item")
	}
	return Item{kind: ItemType("tool_call"), toolCall: value}, nil
}
func NewCustomToolCallItem(value CustomToolCall) (Item, error) {
	if !value.Valid() {
		return Item{}, itemError(CodeInvalidInvariant, "item")
	}
	return Item{kind: ItemType("custom_tool_call"), customCall: value}, nil
}
func NewToolResultItem(value ToolResult) (Item, error) {
	if !value.Valid() {
		return Item{}, itemError(CodeInvalidInvariant, "item")
	}
	return Item{kind: ItemType("tool_result"), toolResult: value}, nil
}
func NewReasoningSummaryItem(value ReasoningSummary) (Item, error) {
	if !value.Valid() {
		return Item{}, itemError(CodeInvalidInvariant, "item")
	}
	return Item{kind: ItemType("reasoning_summary"), reasoning: value}, nil
}

// ID returns the selected item's canonical ID.
func (i Item) ID() ItemID {
	switch i.kind {
	case ItemType("message"):
		return i.message.ID()
	case ItemType("tool_call"):
		return i.toolCall.ID()
	case ItemType("custom_tool_call"):
		return i.customCall.ID()
	case ItemType("tool_result"):
		return i.toolResult.ID()
	case ItemType("reasoning_summary"):
		return i.reasoning.ID()
	default:
		return ""
	}
}

func (i Item) Type() ItemType             { return i.kind }
func (i Item) Message() (Message, bool)   { return i.message, i.kind == ItemType("message") }
func (i Item) ToolCall() (ToolCall, bool) { return i.toolCall, i.kind == ItemType("tool_call") }
func (i Item) CustomToolCall() (CustomToolCall, bool) {
	return i.customCall, i.kind == ItemType("custom_tool_call")
}
func (i Item) ToolResult() (ToolResult, bool) { return i.toolResult, i.kind == ItemType("tool_result") }
func (i Item) ReasoningSummary() (ReasoningSummary, bool) {
	return i.reasoning, i.kind == ItemType("reasoning_summary")
}
func (i Item) Valid() bool {
	switch i.kind {
	case ItemType("message"):
		return i.message.Valid() && !i.toolCall.Valid() && !i.customCall.Valid() && !i.toolResult.Valid() && !i.reasoning.Valid()
	case ItemType("tool_call"):
		return !i.message.Valid() && i.toolCall.Valid() && !i.customCall.Valid() && !i.toolResult.Valid() && !i.reasoning.Valid()
	case ItemType("custom_tool_call"):
		return !i.message.Valid() && !i.toolCall.Valid() && i.customCall.Valid() && !i.toolResult.Valid() && !i.reasoning.Valid()
	case ItemType("tool_result"):
		return !i.message.Valid() && !i.toolCall.Valid() && !i.customCall.Valid() && i.toolResult.Valid() && !i.reasoning.Valid()
	case ItemType("reasoning_summary"):
		return !i.message.Valid() && !i.toolCall.Valid() && !i.customCall.Valid() && !i.toolResult.Valid() && i.reasoning.Valid()
	default:
		return false
	}
}
