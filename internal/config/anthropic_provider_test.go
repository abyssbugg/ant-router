package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func validAnthropicProvider(id, alias string) ProviderConfig {
	return ProviderConfig{
		ID:      id,
		Kind:    ProviderKindAnthropic,
		Enabled: true,
		Credential: &CredentialRef{
			Source: CredentialSourceEnv,
			Name:   "ANTHROPIC_API_KEY",
		},
		Models: []ProviderModelConfig{{
			Alias:                  alias,
			Upstream:               "claude-test",
			Capabilities:           []string{"image_input", "structured_output", "text", "tools"},
			DefaultMaxOutputTokens: 4096,
			MaxOutputTokens:        8192,
		}},
	}
}

func TestDecodeSchemaV4AnthropicProvider(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 4
[[providers]]
id = "anthropic-primary"
kind = "anthropic"
enabled = true
[providers.credential]
source = "env"
name = "ANTHROPIC_API_KEY"
[[providers.models]]
alias = "anthropic/claude-test"
upstream = "claude-test"
capabilities = ["text", "tools", "structured_output", "image_input"]
default_max_output_tokens = 4096
max_output_tokens = 8192
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	want := validAnthropicProvider("anthropic-primary", "anthropic/claude-test")
	if cfg.SchemaVersion != 4 || len(cfg.Providers) != 1 || !reflect.DeepEqual(cfg.Providers[0], want) {
		t.Fatalf("decoded config = %#v, want provider %#v", cfg, want)
	}
}

func TestAnthropicProviderValidation(t *testing.T) {
	valid := Default()
	valid.DataPlane.Enabled = true
	valid.Providers = []ProviderConfig{validAnthropicProvider("anthropic-primary", "anthropic/claude-test")}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	tests := []struct {
		name string
		edit func(*ProviderConfig)
		path string
	}{
		{"missing credential", func(p *ProviderConfig) { p.Credential = nil }, "providers[0].credential"},
		{"missing models", func(p *ProviderConfig) { p.Models = nil }, "providers[0].models"},
		{"missing text", func(p *ProviderConfig) { p.Models[0].Capabilities = []string{"tools"} }, "providers[0].models[0].capabilities"},
		{"reasoning deferred", func(p *ProviderConfig) { p.Models[0].Capabilities = []string{"reasoning_summary", "text"} }, "providers[0].models[0].capabilities[0]"},
		{"continuation deferred", func(p *ProviderConfig) { p.Models[0].Capabilities = []string{"continuation", "text"} }, "providers[0].models[0].capabilities[0]"},
		{"parallel without tools", func(p *ProviderConfig) { p.Models[0].Capabilities = []string{"parallel_tools", "text"} }, "providers[0].models[0].capabilities[0]"},
		{"missing default", func(p *ProviderConfig) { p.Models[0].DefaultMaxOutputTokens = 0 }, "providers[0].models[0].default_max_output_tokens"},
		{"default too large", func(p *ProviderConfig) { p.Models[0].DefaultMaxOutputTokens = 8193 }, "providers[0].models[0].default_max_output_tokens"},
		{"missing maximum", func(p *ProviderConfig) { p.Models[0].MaxOutputTokens = 0 }, "providers[0].models[0].max_output_tokens"},
		{"maximum over canonical limit", func(p *ProviderConfig) { p.Models[0].MaxOutputTokens = 1_000_001 }, "providers[0].models[0].max_output_tokens"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid.clone()
			tt.edit(&cfg.Providers[0])
			err := cfg.Validate()
			var configErr *Error
			if !errors.As(err, &configErr) || configErr.Path != tt.path {
				t.Fatalf("Validate() error = %v, want path %q", err, tt.path)
			}
		})
	}
}

func TestOpenAIProviderRejectsAnthropicTokenLimits(t *testing.T) {
	cfg := Default()
	provider := validOpenAIProvider("openai", "openai/model")
	provider.Models[0].DefaultMaxOutputTokens = 1
	cfg.Providers = []ProviderConfig{provider}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted Anthropic token limits on OpenAI")
	}
}
