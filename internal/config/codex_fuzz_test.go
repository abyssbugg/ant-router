package config

import (
	"strings"
	"testing"
)

func FuzzCodexModelMetadata(f *testing.F) {
	f.Add("GPT Test", "Test model", int64(128000), 90, 10, "list")
	f.Add("", "", int64(0), 0, 0, "")
	f.Fuzz(func(t *testing.T, display, description string, contextWindow int64, percent, priority int, visibility string) {
		cfg := Default()
		provider := validOpenAIProvider("openai-primary", "openai/gpt-test")
		provider.Models[0].Codex = &CodexModelConfig{
			Enabled: true, DisplayName: display, Description: description,
			ContextWindow: contextWindow, EffectiveContextWindowPercent: percent,
			Priority: priority, Visibility: CodexVisibility(visibility),
		}
		cfg.Providers = []ProviderConfig{provider}
		if err := cfg.Validate(); err != nil {
			return
		}
		encoded, err := Canonical(cfg)
		if err != nil {
			t.Fatalf("Canonical() error = %v", err)
		}
		decoded, err := Decode(strings.NewReader(string(encoded)))
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		got := decoded.Providers[0].Models[0].Codex
		if got == nil || *got != *provider.Models[0].Codex {
			t.Fatalf("round trip metadata = %#v", got)
		}
	})
}
