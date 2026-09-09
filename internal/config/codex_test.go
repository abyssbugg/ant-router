package config

import (
	"strings"
	"testing"
)

func validCodexMetadata() *CodexModelConfig {
	return &CodexModelConfig{
		Enabled: true, DisplayName: "GPT Test via Ant Router", Description: "Test model routed through Ant Router",
		ContextWindow: 128000, EffectiveContextWindowPercent: 90, Priority: 10, Visibility: CodexVisibilityList,
	}
}

func configWithCodexMetadata() Config {
	cfg := Default()
	provider := validOpenAIProvider("openai-primary", "openai/gpt-test")
	provider.Models[0].Capabilities = []string{"custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"}
	provider.Models[0].Codex = validCodexMetadata()
	cfg.Providers = []ProviderConfig{provider}
	return cfg
}

func TestCodexMetadataValid(t *testing.T) {
	if err := configWithCodexMetadata().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCodexMetadataValidationPaths(t *testing.T) {
	tests := []struct {
		name, path string
		mutate     func(*CodexModelConfig)
	}{
		{"empty display", "providers[0].models[0].codex.display_name", func(c *CodexModelConfig) { c.DisplayName = "" }},
		{"long display", "providers[0].models[0].codex.display_name", func(c *CodexModelConfig) { c.DisplayName = strings.Repeat("x", MaxCodexDisplayNameBytes+1) }},
		{"padded display", "providers[0].models[0].codex.display_name", func(c *CodexModelConfig) { c.DisplayName = " GPT Test" }},
		{"control display", "providers[0].models[0].codex.display_name", func(c *CodexModelConfig) { c.DisplayName = "GPT\nTest" }},
		{"long description", "providers[0].models[0].codex.description", func(c *CodexModelConfig) { c.Description = strings.Repeat("x", MaxCodexDescriptionBytes+1) }},
		{"control description", "providers[0].models[0].codex.description", func(c *CodexModelConfig) { c.Description = "Test\x00model" }},
		{"zero context", "providers[0].models[0].codex.context_window", func(c *CodexModelConfig) { c.ContextWindow = 0 }},
		{"large context", "providers[0].models[0].codex.context_window", func(c *CodexModelConfig) { c.ContextWindow = MaxCodexContextWindow + 1 }},
		{"zero percent", "providers[0].models[0].codex.effective_context_window_percent", func(c *CodexModelConfig) { c.EffectiveContextWindowPercent = 0 }},
		{"large percent", "providers[0].models[0].codex.effective_context_window_percent", func(c *CodexModelConfig) { c.EffectiveContextWindowPercent = 101 }},
		{"low priority", "providers[0].models[0].codex.priority", func(c *CodexModelConfig) { c.Priority = MinCodexPriority - 1 }},
		{"high priority", "providers[0].models[0].codex.priority", func(c *CodexModelConfig) { c.Priority = MaxCodexPriority + 1 }},
		{"visibility", "providers[0].models[0].codex.visibility", func(c *CodexModelConfig) { c.Visibility = "unknown" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := configWithCodexMetadata()
			tt.mutate(cfg.Providers[0].Models[0].Codex)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.path) {
				t.Fatalf("error = %v, want path %s", err, tt.path)
			}
		})
	}
}

func TestCodexMetadataDisabledMayBeOmitted(t *testing.T) {
	cfg := Default()
	cfg.Providers = []ProviderConfig{validOpenAIProvider("openai-primary", "openai/gpt-test")}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Providers[0].Models[0].Codex != nil {
		t.Fatal("metadata unexpectedly synthesized")
	}
}

func TestCodexMetadataCloneIsDeep(t *testing.T) {
	cfg := configWithCodexMetadata()
	cloned := cfg.clone()
	cfg.Providers[0].Models[0].Codex.DisplayName = "mutated"
	if cloned.Providers[0].Models[0].Codex.DisplayName != "GPT Test via Ant Router" {
		t.Fatal("clone aliases Codex metadata")
	}
	copy := cloned.Providers[0].Models[0].Codex
	copy.Description = "changed"
	if cfg.Providers[0].Models[0].Codex.Description == "changed" {
		t.Fatal("metadata aliases caller state")
	}
}

func TestCodexMetadataCanonicalRoundTrip(t *testing.T) {
	encoded, err := Canonical(configWithCodexMetadata())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Providers[0].Models[0].Codex
	if got == nil || *got != *validCodexMetadata() {
		t.Fatalf("metadata = %#v", got)
	}
	encodedAgain, err := Canonical(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedAgain) != string(encoded) {
		t.Fatal("canonical encoding changed after round trip")
	}
}
