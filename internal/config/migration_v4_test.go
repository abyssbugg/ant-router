package config

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultUsesSchemaV6FromV4Migration(t *testing.T) {
	if got := Default().SchemaVersion; got != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", got, SchemaVersion)
	}
}

func TestSchemaV3MigratesToV6WithoutTokenLimits(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 3
[[providers]]
id = "openai-primary"
kind = "openai"
enabled = true
[providers.credential]
source = "env"
name = "OPENAI_API_KEY"
[[providers.models]]
alias = "openai/gpt-test"
upstream = "gpt-test"
capabilities = ["text"]
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if cfg.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", cfg.SchemaVersion, SchemaVersion)
	}
	model := cfg.Providers[0].Models[0]
	if model.DefaultMaxOutputTokens != 0 || model.MaxOutputTokens != 0 {
		t.Fatalf("migrated token limits = %d/%d, want zero", model.DefaultMaxOutputTokens, model.MaxOutputTokens)
	}
}

func TestSchemaV3RejectsAnthropicTokenFields(t *testing.T) {
	_, err := Decode(strings.NewReader(`schema_version = 3
[[providers]]
id = "openai-primary"
kind = "openai"
enabled = true
[providers.credential]
source = "env"
name = "OPENAI_API_KEY"
[[providers.models]]
alias = "openai/gpt-test"
upstream = "gpt-test"
capabilities = ["text"]
default_max_output_tokens = 1
`))
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Code != CodeUnknownField || configErr.Path != "providers[0].models[0].default_max_output_tokens" {
		t.Fatalf("Decode() error = %#v, want unavailable token field", err)
	}
}

func TestSchemaV3RejectsAnthropicProviderKind(t *testing.T) {
	_, err := Decode(strings.NewReader(`schema_version = 3
[[providers]]
id = "anthropic-primary"
kind = "anthropic"
enabled = true
`))
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Code != CodeValidationFailed || configErr.Path != "providers[0].kind" {
		t.Fatalf("Decode() error = %#v, want unavailable Anthropic kind", err)
	}
}

func TestSchemaV6CanonicalRoundTripAnthropic(t *testing.T) {
	cfg := Default()
	cfg.Providers = []ProviderConfig{validAnthropicProvider("anthropic-primary", "anthropic/claude-test")}
	encoded, err := Canonical(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "schema_version = 8") || !strings.Contains(string(encoded), "kind = 'anthropic'") {
		t.Fatalf("canonical config = %s", encoded)
	}
	decoded, err := Decode(strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Providers[0].Models[0].DefaultMaxOutputTokens != 4096 || decoded.Providers[0].Models[0].MaxOutputTokens != 8192 {
		t.Fatalf("round-trip limits = %#v", decoded.Providers[0].Models[0])
	}
}
