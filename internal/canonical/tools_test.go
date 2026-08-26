package canonical

import (
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestToolDefinitionAndChoice(t *testing.T) {
	schema := testJSONObject(t, `{"type":"object","properties":{"query":{"type":"string"}}}`)
	tool, err := NewToolDefinition("Search_1", "Search the index", schema)
	if err != nil || !tool.Valid() || tool.Name() != "Search_1" || tool.Description() != "Search the index" || !tool.InputSchema().Equal(schema) {
		t.Fatalf("NewToolDefinition() = %#v, %v", tool, err)
	}
	for _, kind := range []ToolChoiceKind{"auto", "none", "required"} {
		choice, err := NewToolChoice(kind, "")
		if err != nil || !choice.Valid() || choice.Kind() != kind || choice.Name() != "" {
			t.Errorf("choice %q = %#v, %v", kind, choice, err)
		}
	}
	specific, err := NewToolChoice(ToolChoiceKind("specific"), "Search_1")
	if err != nil || specific.Name() != "Search_1" || !specific.Valid() {
		t.Errorf("specific choice = %#v, %v", specific, err)
	}
}

func TestToolDefinitionRejectsInvalid(t *testing.T) {
	object := testJSONObject(t, `{}`)
	array, _ := canonicaljson.Parse([]byte(`[]`), canonicaljson.DefaultLimits())
	largeData := []byte(`{"x":"` + strings.Repeat("x", MaxToolSchemaBytes) + `"}`)
	large, err := canonicaljson.Parse(largeData, canonicaljson.Limits{MaximumBytes: len(largeData), MaximumDepth: MaxJSONDepth})
	if err != nil {
		t.Fatalf("large schema parse failed: %v", err)
	}
	tests := []struct {
		name string
		make func() error
		code ErrorCode
	}{
		{"empty name", func() error { _, err := NewToolDefinition("", "", object); return err }, CodeInvalidInvariant},
		{"bad name", func() error { _, err := NewToolDefinition("bad name", "", object); return err }, CodeInvalidInvariant},
		{"leading digit", func() error { _, err := NewToolDefinition("1tool", "", object); return err }, CodeInvalidInvariant},
		{"leading underscore", func() error { _, err := NewToolDefinition("_tool", "", object); return err }, CodeInvalidInvariant},
		{"leading hyphen", func() error { _, err := NewToolDefinition("-tool", "", object); return err }, CodeInvalidInvariant},
		{"long name", func() error {
			_, err := NewToolDefinition(strings.Repeat("a", MaxToolNameBytes+1), "", object)
			return err
		}, CodeLimitExceeded},
		{"description utf8", func() error { _, err := NewToolDefinition("tool", string([]byte{0xff}), object); return err }, CodeInvalidUTF8},
		{"description limit", func() error {
			_, err := NewToolDefinition("tool", strings.Repeat("x", MaxToolDescriptionBytes+1), object)
			return err
		}, CodeLimitExceeded},
		{"schema kind", func() error { _, err := NewToolDefinition("tool", "", array); return err }, CodeInvalidJSON},
		{"schema size", func() error { _, err := NewToolDefinition("tool", "", large); return err }, CodeLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.make(); codeOfCanonical(err) != test.code {
				t.Errorf("code = %q, want %q; error: %v", codeOfCanonical(err), test.code, err)
			}
		})
	}
}

func TestToolChoiceRejectsInvalidCombinations(t *testing.T) {
	for _, kind := range []ToolChoiceKind{"auto", "none", "required"} {
		if _, err := NewToolChoice(kind, "tool"); codeOfCanonical(err) != CodeInvalidInvariant {
			t.Errorf("%q accepted name", kind)
		}
	}
	if _, err := NewToolChoice(ToolChoiceKind("specific"), ""); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Error("specific choice accepted empty name")
	}
	if _, err := NewToolChoice(ToolChoiceKind("other"), ""); codeOfCanonical(err) != CodeInvalidInvariant {
		t.Error("unknown tool choice accepted")
	}
}
