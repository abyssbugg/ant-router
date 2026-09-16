package config

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultUsesSchemaV6FromV5Migration(t *testing.T) {
	if got := Default().SchemaVersion; got != 6 {
		t.Fatalf("schema version = %d, want 6", got)
	}
}

func TestSchemaV4MigratesToV6WithoutCodexMetadata(t *testing.T) {
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
	if cfg.SchemaVersion != 6 {
		t.Fatalf("schema version = %d, want 6", cfg.SchemaVersion)
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

const schemaV5CodexTOML = `schema_version = 5
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
`

const schemaV6CodexTOML = `schema_version = 6
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
`

func TestSchemaV5CodexPayloadMigratesToV6Baseline(t *testing.T) {
	cfg, err := Decode(strings.NewReader(schemaV5CodexTOML))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if cfg.SchemaVersion != 6 || cfg.Providers[0].Models[0].Codex == nil {
		t.Fatalf("migrated config = %#v", cfg)
	}
	want := []string{"custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"}
	if got := cfg.Providers[0].Models[0].Capabilities; !reflect.DeepEqual(got, want) {
		t.Fatalf("migrated capabilities = %v, want %v", got, want)
	}
}

func TestSchemaV5CodexPayloadCanonicalizesAsV6(t *testing.T) {
	cfg, err := Decode(strings.NewReader(schemaV5CodexTOML))
	if err != nil {
		t.Fatal(err)
	}
	native := Default()
	native.Providers = []ProviderConfig{{
		ID:         "openai-primary",
		Kind:       ProviderKindOpenAI,
		Enabled:    true,
		Credential: &CredentialRef{Source: CredentialSourceEnv, Name: "OPENAI_API_KEY"},
		Models: []ProviderModelConfig{{
			Alias:        "openai/gpt-test",
			Upstream:     "gpt-test",
			Capabilities: []string{"custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"},
			Codex: &CodexModelConfig{
				Enabled:                       true,
				DisplayName:                   "GPT Test",
				Description:                   "Test",
				ContextWindow:                 128000,
				EffectiveContextWindowPercent: 90,
				Priority:                      10,
				Visibility:                    CodexVisibilityList,
			},
		}},
	}}
	migrated, err := Canonical(cfg)
	if err != nil {
		t.Fatal(err)
	}
	nativeCanonical, err := Canonical(native)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(migrated, nativeCanonical) {
		t.Fatalf("migrated canonical bytes differ from native v6:\n%s\nvs\n%s", migrated, nativeCanonical)
	}
	roundTrip, err := Decode(strings.NewReader(string(migrated)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip, cfg) {
		t.Fatalf("round trip = %#v, want %#v", roundTrip, cfg)
	}
}

func TestSchemaV6CodexPayloadStillRequiresWrittenBaseline(t *testing.T) {
	_, err := Decode(strings.NewReader(schemaV6CodexTOML))
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Path != "providers[0].models[0].capabilities" {
		t.Fatalf("error = %#v, want schema-v6 capability baseline failure", err)
	}
}

const schemaV5CodexNoCapabilitiesTOML = `schema_version = 5
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
[providers.models.codex]
enabled = true
display_name = "GPT Test"
description = "Test"
context_window = 128000
effective_context_window_percent = 90
priority = 10
visibility = "list"
`

func TestSchemaV5CodexPayloadRejectsEmptyCapabilities(t *testing.T) {
	_, err := Decode(strings.NewReader(schemaV5CodexNoCapabilitiesTOML))
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Code != CodeValidationFailed || configErr.Path != "providers[0].models[0].capabilities" {
		t.Fatalf("error = %#v, want schema-v5 empty capability rejection", err)
	}
}

const schemaV5CodexHostedSearchWithoutToolsTOML = `schema_version = 5
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
capabilities = ["hosted_web_search"]
[providers.models.codex]
enabled = true
display_name = "GPT Test"
description = "Test"
context_window = 128000
effective_context_window_percent = 90
priority = 10
visibility = "list"
`

func TestSchemaV5CodexPayloadRejectsHostedSearchWithoutTools(t *testing.T) {
	_, err := Decode(strings.NewReader(schemaV5CodexHostedSearchWithoutToolsTOML))
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Code != CodeValidationFailed || configErr.Path != "providers[0].models[0].capabilities[0]" {
		t.Fatalf("error = %#v, want schema-v5 hosted web search rejection", err)
	}
}

func TestSchemaV6DecodesCodexMetadata(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 6
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
capabilities = ["custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"]
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
	if cfg.SchemaVersion != 6 || cfg.Providers[0].Models[0].Codex == nil {
		t.Fatalf("config = %#v", cfg)
	}
}
