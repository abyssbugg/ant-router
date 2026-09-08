package codexcatalog

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/config"
)

func projection(alias string, priority int, capabilities ...string) Projection {
	return Projection{Alias: alias, Capabilities: capabilities, Metadata: config.CodexModelConfig{
		Enabled: true, DisplayName: alias + " display", Description: alias + " description",
		ContextWindow: 128000, EffectiveContextWindowPercent: 90, Priority: priority, Visibility: config.CodexVisibilityList,
	}}
}

func TestGenerateUsesOfficialCodexModelInfoFields(t *testing.T) {
	encoded, err := Generate([]Projection{projection("openai/gpt-test", 10, "text", "image_input", "reasoning_summary")})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Models) != 1 {
		t.Fatalf("models = %d", len(response.Models))
	}
	model := response.Models[0]
	checks := map[string]any{"slug": "openai/gpt-test", "display_name": "openai/gpt-test display", "description": "openai/gpt-test description", "shell_type": "unified_exec", "visibility": "list", "supported_in_api": true, "tool_mode": "direct", "supports_search_tool": false, "node_repl_disabled": true, "comp_hash": "ant-router-codex-v1"}
	for key, want := range checks {
		if got := model[key]; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
	for _, required := range []string{"supported_reasoning_levels", "availability_nux", "upgrade", "model_messages", "support_verbosity", "default_verbosity", "apply_patch_tool_type", "truncation_policy", "experimental_supported_tools", "input_modalities", "base_instructions"} {
		if _, ok := model[required]; !ok {
			t.Fatalf("missing %s", required)
		}
	}
	for _, forbidden := range []string{"id", "object", "created", "owned_by", "display", "context", "effective_percent", "provider_instructions", "image_modality", "reasoning_summary", "code_mode", "search_mode", "multi_agent"} {
		if _, ok := model[forbidden]; ok {
			t.Fatalf("invented field %s present", forbidden)
		}
	}
	if model["apply_patch_tool_type"] != nil || model["default_reasoning_level"] != nil || model["multi_agent_version"] != nil {
		t.Fatal("unsupported catalog feature enabled")
	}
	if got := model["input_modalities"].([]any); len(got) != 2 || got[0] != "text" || got[1] != "image" {
		t.Fatalf("modalities = %#v", got)
	}
	if model["supports_reasoning_summary_parameter"] != true || model["default_reasoning_summary"] != "auto" {
		t.Fatal("reasoning summary metadata missing")
	}
}

func TestGenerateUsesConservativeFunctionToolSettings(t *testing.T) {
	encoded, err := Generate([]Projection{projection("anthropic/sonnet", 1, "text", "tools")})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"freeform"`)) || bytes.Contains(encoded, []byte(`"code_mode"`)) {
		t.Fatalf("unsafe tool mode in %s", encoded)
	}
	if !bytes.Contains(encoded, []byte(`"shell_type": "unified_exec"`)) || !bytes.Contains(encoded, []byte(`"tool_mode": "direct"`)) {
		t.Fatalf("function tool settings missing: %s", encoded)
	}
}

func TestGenerateSortsByPriorityThenAlias(t *testing.T) {
	encoded, err := Generate([]Projection{projection("z/model", 1), projection("b/model", 5), projection("a/model", 5)})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	got := []string{response.Models[0].Slug, response.Models[1].Slug, response.Models[2].Slug}
	want := []string{"a/model", "b/model", "z/model"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %#v", got)
		}
	}
}

func TestGenerateRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		input    []Projection
		contains string
	}{
		{"empty", nil, "no models"},
		{"duplicate", []Projection{projection("a/model", 1), projection("a/model", 2)}, "duplicate"},
		{"disabled", []Projection{{Alias: "a/model", Metadata: config.CodexModelConfig{Enabled: false}}}, "disabled"},
		{"invalid metadata", []Projection{{Alias: "a/model", Metadata: config.CodexModelConfig{Enabled: true}}}, "metadata"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Generate(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestGenerateAcceptsNamespaceToolsCapability(t *testing.T) {
	value := projection("mock/codex", 1, "namespace_tools", "text", "tools")
	if _, err := Generate([]Projection{value}); err != nil {
		t.Fatalf("namespace capability rejected: %v", err)
	}
}

func TestGenerateRejectsUnknownCapabilities(t *testing.T) {
	input := projection("openai/gpt-test", 1, "text", "future_secret")
	_, err := Generate([]Projection{input})
	if err == nil || !strings.Contains(err.Error(), "capabilities") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "future_secret") {
		t.Fatal("error leaked capability value")
	}
}

func TestGenerateIsDeterministicAndSecretFree(t *testing.T) {
	input := []Projection{projection("openai/gpt-test", 10, "text")}
	first, err := Generate(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("catalog is not deterministic")
	}
	for _, secret := range [][]byte{[]byte("OPENAI_API_KEY"), []byte("sk-secret"), []byte("127.0.0.1:8787")} {
		if bytes.Contains(first, secret) {
			t.Fatalf("catalog leaked %q", secret)
		}
	}
	if first[len(first)-1] != '\n' {
		t.Fatal("catalog lacks final newline")
	}
}
