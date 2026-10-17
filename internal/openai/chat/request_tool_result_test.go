package chat

import (
	"bytes"
	"errors"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// TestToolResultIsErrorRejected ensures that tool results with IsError() == true
// are rejected during encoding, matching the responses mapper behavior.
func TestToolResultIsErrorRejected(t *testing.T) {
	callID, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{1}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}

	// Create a tool result that is an error
	errorContent, err := canonical.NewTextContent("Tool execution failed")
	if err != nil {
		t.Fatal(err)
	}
	errorBlock, err := canonical.NewTextBlock(errorContent)
	if err != nil {
		t.Fatal(err)
	}

	// NewToolResult with error=true
	result, err := canonical.NewToolResult(fixedID(t, 1), callID, []canonical.ContentBlock{errorBlock}, true)
	if err != nil {
		t.Fatal(err)
	}
	resultItem, err := canonical.NewToolResultItem(result)
	if err != nil {
		t.Fatal(err)
	}

	items := []canonical.Item{resultItem}

	messages, err := encodeMessages(items)
	if err == nil {
		t.Errorf("encodeMessages with error result should fail, got %v", messages)
	}

	// Verify the error has correct type, code, and path
	var target *Error
	if !errors.As(err, &target) || target.Code != "unsupported" || target.Path != "request.input[0].is_error" {
		t.Fatalf("expected unsupported error at request.input[0].is_error, got: %v (type: %T, code: %v, path: %v)", err, target, target.Code, target.Path)
	}
}

// TestToolResultSuccessfulAllowed ensures that tool results with IsError() == false
// encode successfully (existing behavior).
func TestToolResultSuccessfulAllowed(t *testing.T) {
	callID, err := canonical.GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{1}, canonical.IDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}

	// Create a tool result that is NOT an error
	successContent, err := canonical.NewTextContent("Tool execution succeeded")
	if err != nil {
		t.Fatal(err)
	}
	successBlock, err := canonical.NewTextBlock(successContent)
	if err != nil {
		t.Fatal(err)
	}

	// NewToolResult with error=false
	result, err := canonical.NewToolResult(fixedID(t, 1), callID, []canonical.ContentBlock{successBlock}, false)
	if err != nil {
		t.Fatal(err)
	}
	resultItem, err := canonical.NewToolResultItem(result)
	if err != nil {
		t.Fatal(err)
	}

	items := []canonical.Item{resultItem}

	messages, err := encodeMessages(items)
	if err != nil {
		t.Fatalf("encodeMessages with successful result should not fail: %v", err)
	}

	// Should have exactly 1 message with tool role
	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}

	if len(messages) >= 1 {
		if messages[0]["role"] != "tool" {
			t.Errorf("message[0]: expected role tool, got %v", messages[0]["role"])
		}
		if messages[0]["content"] != "Tool execution succeeded" {
			t.Errorf("message[0].content: expected 'Tool execution succeeded', got %v", messages[0]["content"])
		}
	}
}
