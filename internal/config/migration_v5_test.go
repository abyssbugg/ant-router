package config

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultUsesSchemaV5(t *testing.T) {
	if got := Default().SchemaVersion; got != 5 {
		t.Fatalf("schema version = %d, want 5", got)
	}
}

func TestSchemaV4MigratesToV5WithoutCodexMetadata(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 4
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
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 5 {
		t.Fatalf("schema version = %d, want 5", cfg.SchemaVersion)
	}
	if cfg.Providers[0].Models[0].Codex != nil {
		t.Fatal("migration guessed Codex metadata")
	}
}

func TestSchemaV3RejectsCodexMetadata(t *testing.T) {
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
[providers.models.codex]
enabled = true
display_name = "GPT Test"
description = "Test"
context_window = 128000
effective_context_window_percent = 90
priority = 10
visibility = "list"
`))
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Code != CodeUnknownField || configErr.Path != "providers[0].models[0].codex" {
		t.Fatalf("error = %#v, want schema-v3 codex unknown field", err)
	}
}

func TestSchemaV4RejectsCodexMetadata(t *testing.T) {
	_, err := Decode(strings.NewReader(`schema_version = 4
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
[providers.models.codex]
enabled = true
display_name = "GPT Test"
description = "Test"
context_window = 128000
effective_context_window_percent = 90
priority = 10
visibility = "list"
`))
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Code != CodeUnknownField || configErr.Path != "providers[0].models[0].codex" {
		t.Fatalf("error = %#v, want schema-v4 codex unknown field", err)
	}
}

func TestSchemaV5DecodesCodexMetadata(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 5
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
[providers.models.codex]
enabled = true
display_name = "GPT Test via Ant Router"
description = "Test model routed through Ant Router"
context_window = 128000
effective_context_window_percent = 90
priority = 10
visibility = "list"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 5 || cfg.Providers[0].Models[0].Codex == nil {
		t.Fatalf("config = %#v", cfg)
	}
}
