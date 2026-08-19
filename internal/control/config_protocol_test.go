package control

import (
	"encoding/json"
	"testing"
)

func TestConfigProtocolJSONFields(t *testing.T) {
	value := ConfigApplyRequest{TOML: "schema_version = 1\n", ExpectedRevision: "abc", ExpectedGeneration: 4}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	for _, field := range []string{"toml", "expected_revision", "expected_generation"} {
		if _, ok := fields[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}
