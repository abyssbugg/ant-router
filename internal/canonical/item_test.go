package canonical

import "testing"

func TestItemVariants(t *testing.T) {
	message, _ := NewMessage(testItemID(t, 1), Role("user"), []ContentBlock{testTextBlock(t, "hello")})
	messageItem, err := NewMessageItem(message)
	if err != nil || messageItem.Type() != ItemType("message") || !messageItem.Valid() {
		t.Fatalf("message item = %#v, %v", messageItem, err)
	}
	if got, ok := messageItem.Message(); !ok || got.ID() != message.ID() {
		t.Errorf("Message() = %#v, %v", got, ok)
	}

	call, _ := NewToolCall(testItemID(t, 2), testCallID(t, 2), "tool", testJSONObject(t, `{}`))
	callItem, _ := NewToolCallItem(call)
	if got, ok := callItem.ToolCall(); !ok || got.CallID() != call.CallID() {
		t.Errorf("ToolCall() = %#v, %v", got, ok)
	}

	result, _ := NewToolResult(testItemID(t, 3), testCallID(t, 2), []ContentBlock{testTextBlock(t, "result")}, false)
	resultItem, _ := NewToolResultItem(result)
	if got, ok := resultItem.ToolResult(); !ok || got.CallID() != result.CallID() {
		t.Errorf("ToolResult() = %#v, %v", got, ok)
	}

	text, _ := NewTextContent("summary")
	summary, _ := NewReasoningSummary(testItemID(t, 4), []TextContent{text})
	summaryItem, _ := NewReasoningSummaryItem(summary)
	if got, ok := summaryItem.ReasoningSummary(); !ok || got.ID() != summary.ID() {
		t.Errorf("ReasoningSummary() = %#v, %v", got, ok)
	}
}

func TestItemRejectsZeroVariants(t *testing.T) {
	if (Item{}).Valid() {
		t.Error("zero Item is valid")
	}
	if _, err := NewMessageItem(Message{}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Error("zero message accepted")
	}
	if _, err := NewToolCallItem(ToolCall{}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Error("zero tool call accepted")
	}
	if _, err := NewToolResultItem(ToolResult{}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Error("zero tool result accepted")
	}
	if _, err := NewReasoningSummaryItem(ReasoningSummary{}); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Error("zero reasoning summary accepted")
	}
}
