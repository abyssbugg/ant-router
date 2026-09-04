package config

import (
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func FuzzProviderIdentifiers(f *testing.F) {
	for _, seed := range []string{"openai", "openai-primary", "openai/gpt-test", "../secret", "mock/text", "", "OPENAI"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		_ = validProviderID(value)
		_ = validModelIdentifier(value)
		if len(value) > MaxModelIDBytes || !validModelIdentifier(value) {
			return
		}
		cfg := Default()
		provider := validOpenAIProvider("openai", value)
		provider.Models[0].Upstream = value
		cfg.Providers = []ProviderConfig{provider}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("valid model identifier %q failed validation: %v", value, err)
		}
	})
}

func FuzzAnthropicTokenLimits(f *testing.F) {
	for _, seed := range [][2]int64{{1, 1}, {4096, 8192}, {0, 1}, {2, 1}, {1, canonical.MaxOutputTokens + 1}} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, defaultTokens, maxTokens int64) {
		provider := validAnthropicProvider("anthropic", "anthropic/model")
		provider.Models[0].DefaultMaxOutputTokens = defaultTokens
		provider.Models[0].MaxOutputTokens = maxTokens
		cfg := Default()
		cfg.Providers = []ProviderConfig{provider}
		err := cfg.Validate()
		valid := defaultTokens > 0 && maxTokens > 0 && defaultTokens <= maxTokens && maxTokens <= canonical.MaxOutputTokens
		if valid && err != nil {
			t.Fatalf("valid token limits %d/%d failed validation: %v", defaultTokens, maxTokens, err)
		}
		if !valid && err == nil {
			t.Fatalf("invalid token limits %d/%d passed validation", defaultTokens, maxTokens)
		}
	})
}

func FuzzCredentialEnvironmentName(f *testing.F) {
	for _, seed := range []string{"OPENAI_API_KEY", "_KEY", "openai_key", "OPENAI-KEY", "", "A"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		provider := validOpenAIProvider("openai", "openai/model")
		provider.Credential.Name = value
		cfg := Default()
		cfg.Providers = []ProviderConfig{provider}
		err := cfg.Validate()
		valid := len(value) > 0 && len(value) <= MaxCredentialName && envNamePattern.MatchString(value)
		if valid && err != nil {
			t.Fatalf("valid environment name %q failed validation: %v", value, err)
		}
		if !valid && err == nil {
			t.Fatalf("invalid environment name %q passed validation", value)
		}
	})
}
