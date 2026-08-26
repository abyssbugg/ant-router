package canonical

import (
	"bytes"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func testEventID(t *testing.T, value byte) EventID {
	t.Helper()
	id, err := GenerateEventID(bytes.NewReader(bytes.Repeat([]byte{value}, IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateEventID() failed: %v", err)
	}
	return id
}

func testResponseID(t *testing.T, value byte) ResponseID {
	t.Helper()
	id, err := GenerateResponseID(bytes.NewReader(bytes.Repeat([]byte{value}, IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateResponseID() failed: %v", err)
	}
	return id
}

func testRequestID(t *testing.T, value byte) RequestID {
	t.Helper()
	id, err := GenerateRequestID(bytes.NewReader(bytes.Repeat([]byte{value}, IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateRequestID() failed: %v", err)
	}
	return id
}

func testItemID(t *testing.T, value byte) ItemID {
	t.Helper()
	id, err := GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{value}, IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateItemID() failed: %v", err)
	}
	return id
}

func testCallID(t *testing.T, value byte) ToolCallID {
	t.Helper()
	id, err := GenerateToolCallID(bytes.NewReader(bytes.Repeat([]byte{value}, IDRandomBytes)))
	if err != nil {
		t.Fatalf("GenerateToolCallID() failed: %v", err)
	}
	return id
}

func testJSONObject(t *testing.T, source string) canonicaljson.Value {
	t.Helper()
	value, err := canonicaljson.Parse([]byte(source), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatalf("canonicaljson.Parse() failed: %v", err)
	}
	return value
}

func testTextBlock(t *testing.T, text string) ContentBlock {
	t.Helper()
	content, err := NewTextContent(text)
	if err != nil {
		t.Fatalf("NewTextContent() failed: %v", err)
	}
	block, err := NewTextBlock(content)
	if err != nil {
		t.Fatalf("NewTextBlock() failed: %v", err)
	}
	return block
}
