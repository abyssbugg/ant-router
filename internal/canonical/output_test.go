package canonical

import (
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func TestOutputConstraints(t *testing.T) {
	text, err := NewOutputConstraints(OutputParams{Format: OutputFormat("text"), MaxTokens: 100, ParallelToolCalls: true})
	if err != nil || !text.Valid() || text.Format() != OutputFormat("text") || text.MaxTokens() != 100 || !text.ParallelToolCalls() {
		t.Fatalf("text output = %#v, %v", text, err)
	}
	schema := testJSONObject(t, `{"type":"object"}`)
	structured, err := NewOutputConstraints(OutputParams{Format: OutputFormat("json_schema"), SchemaName: "Answer", Schema: schema})
	if err != nil || !structured.Valid() || structured.SchemaName() != "Answer" || !structured.Schema().Equal(schema) {
		t.Fatalf("schema output = %#v, %v", structured, err)
	}
}

func TestOutputConstraintsRejectInvalid(t *testing.T) {
	object := testJSONObject(t, `{}`)
	array, _ := canonicaljson.Parse([]byte(`[]`), canonicaljson.DefaultLimits())
	tests := []OutputParams{
		{Format: OutputFormat("other")},
		{Format: OutputFormat("text"), Schema: object},
		{Format: OutputFormat("json_object"), SchemaName: "name"},
		{Format: OutputFormat("json_schema")},
		{Format: OutputFormat("json_schema"), SchemaName: "1bad", Schema: object},
		{Format: OutputFormat("json_schema"), SchemaName: "Answer", Schema: array},
		{Format: OutputFormat("text"), MaxTokens: -1},
		{Format: OutputFormat("text"), MaxTokens: MaxOutputTokens + 1},
		{Format: OutputFormat("json_schema"), SchemaName: strings.Repeat("a", MaxSchemaNameBytes+1), Schema: object},
	}
	for _, params := range tests {
		if _, err := NewOutputConstraints(params); err == nil {
			t.Errorf("invalid output accepted: %#v", params)
		}
	}
}
