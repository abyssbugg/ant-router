package config

import "testing"

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
