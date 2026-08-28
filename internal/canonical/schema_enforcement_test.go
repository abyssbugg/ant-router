package canonical

import (
	"bytes"
	"testing"
)

func TestSchemaEnforcementValues(t *testing.T) {
	if !SchemaEnforcementBestEffort.Valid() || !SchemaEnforcementStrict.Valid() || SchemaEnforcement("unknown").Valid() {
		t.Fatal("schema enforcement validity mismatch")
	}
}

func TestToolDefinitionSchemaEnforcement(t *testing.T) {
	schema := testJSONObject(t, `{"type":"object"}`)
	bestEffort, err := NewToolDefinition("Search", "", schema)
	if err != nil || bestEffort.Enforcement() != SchemaEnforcementBestEffort {
		t.Fatalf("default tool enforcement = %q, %v", bestEffort.Enforcement(), err)
	}
	strict, err := NewToolDefinitionWithEnforcement("Search", "", schema, SchemaEnforcementStrict)
	if err != nil || !strict.Valid() || strict.Enforcement() != SchemaEnforcementStrict {
		t.Fatalf("strict tool = %#v, %v", strict, err)
	}
	if _, err := NewToolDefinitionWithEnforcement("Search", "", schema, SchemaEnforcement("unknown")); err == nil {
		t.Fatal("unknown tool enforcement accepted")
	}
}

func TestOutputSchemaEnforcement(t *testing.T) {
	schema := testJSONObject(t, `{"type":"object"}`)
	bestEffort, err := NewOutputConstraints(OutputParams{Format: OutputFormat("json_schema"), SchemaName: "Answer", Schema: schema})
	if err != nil || bestEffort.Enforcement() != SchemaEnforcementBestEffort {
		t.Fatalf("default output enforcement = %q, %v", bestEffort.Enforcement(), err)
	}
	strict, err := NewOutputConstraints(OutputParams{Format: OutputFormat("json_schema"), SchemaName: "Answer", Schema: schema, Enforcement: SchemaEnforcementStrict})
	if err != nil || !strict.Valid() || strict.Enforcement() != SchemaEnforcementStrict {
		t.Fatalf("strict output = %#v, %v", strict, err)
	}
	for _, params := range []OutputParams{
		{Format: OutputFormat("json_schema"), SchemaName: "Answer", Schema: schema, Enforcement: SchemaEnforcement("unknown")},
		{Format: OutputFormat("text"), Enforcement: SchemaEnforcementStrict},
		{Format: OutputFormat("json_object"), Enforcement: SchemaEnforcementStrict},
	} {
		if _, err := NewOutputConstraints(params); err == nil {
			t.Fatalf("invalid enforcement accepted: %#v", params)
		}
	}
}

func TestRequestCodecDefaultsMissingSchemaEnforcement(t *testing.T) {
	encoded, err := EncodeRequest(codecRequest(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	legacy := bytes.ReplaceAll(encoded, []byte(`,"schema_enforcement":"best_effort"`), nil)
	if bytes.Equal(legacy, encoded) {
		t.Fatal("test fixture did not remove enforcement fields")
	}
	decoded, err := DecodeRequest(legacy)
	if err != nil {
		t.Fatalf("DecodeRequest(legacy) failed: %v", err)
	}
	if decoded.Tools()[0].Enforcement() != SchemaEnforcementBestEffort || decoded.Output().Enforcement() != SchemaEnforcementBestEffort {
		t.Fatal("missing enforcement did not default to best effort")
	}
}

func TestRequestCodecPreservesSchemaEnforcement(t *testing.T) {
	base := codecRequest(t, 1)
	baseTool := base.Tools()[0]
	strictTool, err := NewToolDefinitionWithEnforcement(baseTool.Name(), baseTool.Description(), baseTool.InputSchema(), SchemaEnforcementStrict)
	if err != nil {
		t.Fatal(err)
	}
	baseOutput := base.Output()
	strictOutput, err := NewOutputConstraints(OutputParams{
		Format: baseOutput.Format(), SchemaName: baseOutput.SchemaName(), Schema: baseOutput.Schema(),
		MaxTokens: baseOutput.MaxTokens(), ParallelToolCalls: baseOutput.ParallelToolCalls(), Enforcement: SchemaEnforcementStrict,
	})
	if err != nil {
		t.Fatal(err)
	}
	strict, err := NewRequest(RequestParams{
		ID: base.ID(), Model: base.Model(), Input: base.Input(), Tools: []ToolDefinition{strictTool},
		ToolChoice: base.ToolChoice(), Output: strictOutput, Sampling: base.Sampling(), Continuation: base.Continuation(),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeRequest(strict)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"schema_enforcement":"strict"`)) {
		t.Fatalf("strict enforcement missing: %s", encoded)
	}
	decoded, err := DecodeRequest(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Tools()[0].Enforcement() != SchemaEnforcementStrict || decoded.Output().Enforcement() != SchemaEnforcementStrict {
		t.Fatal("strict enforcement did not round trip")
	}
	bestDigest, _ := FullDigest(base)
	strictDigest, _ := FullDigest(strict)
	if bestDigest == strictDigest {
		t.Fatal("schema enforcement did not affect request digest")
	}
}
