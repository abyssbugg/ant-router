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
		provider.Models[0].Capabilities = []string{"custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"}
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

func FuzzCodexCapabilityBaseline(f *testing.F) {
	f.Add(true, true, true, true, true, true)
	f.Add(false, true, true, true, true, true)
	f.Fuzz(func(t *testing.T, custom, hosted, namespace, parallel, text, tools bool) {
		capabilities := make([]string, 0, 6)
		if custom {
			capabilities = append(capabilities, "custom_tools")
		}
		if hosted {
			capabilities = append(capabilities, "hosted_web_search")
		}
		if namespace {
			capabilities = append(capabilities, "namespace_tools")
		}
		if parallel {
			capabilities = append(capabilities, "parallel_tools")
		}
		if text {
			capabilities = append(capabilities, "text")
		}
		if tools {
			capabilities = append(capabilities, "tools")
		}
		cfg := Default()
		provider := validOpenAIProvider("openai-primary", "openai/gpt-test")
		provider.Models[0].Capabilities = capabilities
		provider.Models[0].Codex = validCodexMetadata()
		cfg.Providers = []ProviderConfig{provider}
		err := cfg.Validate()
		valid := custom && hosted && namespace && parallel && text && tools
		if valid && err != nil {
			t.Fatalf("complete baseline rejected: %v", err)
		}
		if !valid && err == nil {
			t.Fatalf("incomplete baseline accepted: %v", capabilities)
		}
	})
}
