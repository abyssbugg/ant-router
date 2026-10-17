package chat

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func textBlock(t *testing.T, content string) canonical.ContentBlock {
	t.Helper()
	text, err := canonical.NewTextContent(content)
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}
	return block
}

func fixedID(t *testing.T, value byte) canonical.ItemID {
	t.Helper()
	id, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{value}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestReasoningSummaryOmitted ensures reasoning_summary items in history are skipped
// during encoding, and that tool calls around reasoning items still group correctly.
func TestReasoningSummaryOmitted(t *testing.T) {
	// Build history: [user message, reasoning_summary, assistant message, user message]
	userMsg1, err := canonical.NewMessage(fixedID(t, 1), canonical.Role("user"), []canonical.ContentBlock{textBlock(t, "First question")})
	if err != nil {
		t.Fatal(err)
	}
	item1, err := canonical.NewMessageItem(userMsg1)
	if err != nil {
		t.Fatal(err)
	}

	// Create a reasoning summary item
	reasoningText, err := canonical.NewTextContent("I should think about this")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningSummary(fixedID(t, 2), []canonical.TextContent{reasoningText})
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem, err := canonical.NewReasoningSummaryItem(reasoning)
	if err != nil {
		t.Fatal(err)
	}

	// Assistant message with content (no tool calls)
	assistantMsg, err := canonical.NewMessage(fixedID(t, 3), canonical.Role("assistant"), []canonical.ContentBlock{textBlock(t, "Let me help")})
	if err != nil {
		t.Fatal(err)
	}
	assistantMsgItem, err := canonical.NewMessageItem(assistantMsg)
	if err != nil {
		t.Fatal(err)
	}

	userMsg2, err := canonical.NewMessage(fixedID(t, 4), canonical.Role("user"), []canonical.ContentBlock{textBlock(t, "Follow up question")})
	if err != nil {
		t.Fatal(err)
	}
	item2, err := canonical.NewMessageItem(userMsg2)
	if err != nil {
		t.Fatal(err)
	}

	items := []canonical.Item{item1, reasoningItem, assistantMsgItem, item2}

	messages, err := encodeMessages(items)
	if err != nil {
		t.Fatalf("encodeMessages failed: %v", err)
	}

	// Should have exactly 3 messages (reasoning omitted)
	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}

	// Verify structure: user, assistant, user
	if len(messages) >= 1 && messages[0]["role"] != "user" {
		t.Errorf("message[0]: expected role user, got %v", messages[0]["role"])
	}
	if len(messages) >= 2 && messages[1]["role"] != "assistant" {
		t.Errorf("message[1]: expected role assistant, got %v", messages[1]["role"])
	}
	if len(messages) >= 3 && messages[2]["role"] != "user" {
		t.Errorf("message[2]: expected role user, got %v", messages[2]["role"])
	}

	// Verify assistant message content is exactly "Let me help"
	if len(messages) >= 2 {
		assistantContent, ok := messages[1]["content"].(string)
		if !ok || assistantContent != "Let me help" {
			t.Errorf("message[1].content: expected 'Let me help', got %v", messages[1]["content"])
		}
	}

	// Verify no message contains the reasoning text
	for i, msg := range messages {
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal message[%d]: %v", i, err)
		}
		msgStr := string(msgJSON)
		if strings.Contains(msgStr, "I should think about this") {
			t.Errorf("message[%d] contains reasoning text: %s", i, msgStr)
		}
	}
}

// TestToolCallGroupingAroundReasoning ensures that tool calls that would be
// separated by a reasoning_summary item still group in the same assistant message.
func TestToolCallGroupingAroundReasoning(t *testing.T) {
	// Build history: [assistant tool_call, reasoning_summary, assistant tool_call]
	// These should both add to the same assistant message with tool_calls array

	callID1, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{1}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}

	jsonVal, err := canonicaljson.Parse([]byte(`{"x": 1}`), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	call1, err := canonical.NewToolCall(fixedID(t, 1), callID1, "test_func", jsonVal)
	if err != nil {
		t.Fatal(err)
	}
	toolItem1, err := canonical.NewToolCallItem(call1)
	if err != nil {
		t.Fatal(err)
	}

	// Reasoning in the middle
	reasoningText, err := canonical.NewTextContent("Reasoning between calls")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningSummary(fixedID(t, 2), []canonical.TextContent{reasoningText})
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem, err := canonical.NewReasoningSummaryItem(reasoning)
	if err != nil {
		t.Fatal(err)
	}

	callID2, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{2}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}

	jsonVal2, err := canonicaljson.Parse([]byte(`{"y": 2}`), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	call2, err := canonical.NewToolCall(fixedID(t, 3), callID2, "another_func", jsonVal2)
	if err != nil {
		t.Fatal(err)
	}
	toolItem2, err := canonical.NewToolCallItem(call2)
	if err != nil {
		t.Fatal(err)
	}

	items := []canonical.Item{toolItem1, reasoningItem, toolItem2}

	messages, err := encodeMessages(items)
	if err != nil {
		t.Fatalf("encodeMessages failed: %v", err)
	}

	// Should have exactly 1 message (the reasoning is skipped, both tool calls group)
	if len(messages) != 1 {
		t.Errorf("expected 1 message (both tool calls grouped), got %d", len(messages))
	}

	if len(messages) >= 1 {
		if messages[0]["role"] != "assistant" {
			t.Errorf("message[0]: expected role assistant, got %v", messages[0]["role"])
		}

		toolCalls, ok := messages[0]["tool_calls"].([]map[string]any)
		if !ok {
			t.Fatalf("message[0].tool_calls: expected []map[string]any, got %T", messages[0]["tool_calls"])
		}

		if len(toolCalls) != 2 {
			t.Errorf("expected 2 tool_calls, got %d", len(toolCalls))
		}

		// Assert exact IDs and names
		if len(toolCalls) >= 1 {
			id1, ok := toolCalls[0]["id"].(string)
			if !ok || id1 != string(callID1) {
				t.Errorf("toolCalls[0].id: expected %q, got %q", string(callID1), id1)
			}
			name1, ok := toolCalls[0]["function"].(map[string]any)["name"].(string)
			if !ok || name1 != "test_func" {
				t.Errorf("toolCalls[0].function.name: expected 'test_func', got %q", name1)
			}
		}
		if len(toolCalls) >= 2 {
			id2, ok := toolCalls[1]["id"].(string)
			if !ok || id2 != string(callID2) {
				t.Errorf("toolCalls[1].id: expected %q, got %q", string(callID2), id2)
			}
			name2, ok := toolCalls[1]["function"].(map[string]any)["name"].(string)
			if !ok || name2 != "another_func" {
				t.Errorf("toolCalls[1].function.name: expected 'another_func', got %q", name2)
			}
		}
	}
}
