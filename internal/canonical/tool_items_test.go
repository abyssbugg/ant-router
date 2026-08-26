package canonical

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestToolCallAndResult(t *testing.T) {
	arguments := testJSONObject(t, `{"query":"hello"}`)
	call, err := NewToolCall(testItemID(t, 2), testCallID(t, 2), "search", arguments)
	if err != nil || !call.Valid() || call.Name() != "search" || !call.Arguments().Equal(arguments) {
		t.Fatalf("NewToolCall() = %#v, %v", call, err)
	}
	blocks := []ContentBlock{testTextBlock(t, "result")}
	result, err := NewToolResult(testItemID(t, 3), testCallID(t, 2), blocks, true)
	if err != nil || !result.Valid() || !result.IsError() {
		t.Fatalf("NewToolResult() = %#v, %v", result, err)
	}
	blocks[0] = ContentBlock{}
	if content := result.Content(); len(content) != 1 || !content[0].Valid() {
		t.Error("tool result content was mutated")
	}
}

func TestToolCallRejectsInvalid(t *testing.T) {
	object := testJSONObject(t, `{}`)
	array, _ := canonicaljson.Parse([]byte(`[]`), canonicaljson.DefaultLimits())
	tests := []struct {
		name string
		item ItemID
		call ToolCallID
		tool string
		args canonicaljson.Value
		code ErrorCode
	}{
		{"item", "", testCallID(t, 1), "tool", object, CodeInvalidID},
		{"call", testItemID(t, 1), "", "tool", object, CodeInvalidID},
		{"name", testItemID(t, 1), testCallID(t, 1), "bad name", object, CodeInvalidInvariant},
		{"arguments", testItemID(t, 1), testCallID(t, 1), "tool", array, CodeInvalidJSON},
	}
	for _, test := range tests {
		if _, err := NewToolCall(test.item, test.call, test.tool, test.args); codeOfCanonical(err) != test.code {
			t.Errorf("%s code = %q, want %q", test.name, codeOfCanonical(err), test.code)
		}
	}
}

func TestToolResultRejectsInvalid(t *testing.T) {
	valid := testTextBlock(t, "result")
	if _, err := NewToolResult("", testCallID(t, 1), []ContentBlock{valid}, false); codeOfCanonical(err) != CodeInvalidID {
		t.Error("invalid item ID accepted")
	}
	if _, err := NewToolResult(testItemID(t, 1), "", []ContentBlock{valid}, false); codeOfCanonical(err) != CodeInvalidID {
		t.Error("invalid call ID accepted")
	}
	if _, err := NewToolResult(testItemID(t, 1), testCallID(t, 1), nil, false); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Error("empty result accepted")
	}
}
