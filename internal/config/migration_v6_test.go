package config

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const openAICodexMetadataTOML = `[providers.models.codex]
enabled = true
display_name = "GPT Test via Ant Router"
description = "Test model routed through Ant Router"
context_window = 128000
effective_context_window_percent = 90
priority = 10
visibility = "list"
`

func openAIVersionedTOML(version int, capabilities string, withCodex bool) string {
	result := fmt.Sprintf(`schema_version = %d
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
capabilities = [%s]
`, version, capabilities)
	if withCodex {
		result += openAICodexMetadataTOML
	}
	return result
}

func TestDefaultUsesSchemaV6(t *testing.T) {
	if got := Default().SchemaVersion; got != 6 {
		t.Fatalf("schema version = %d, want 6", got)
	}
}

func TestSchemaV5MigratesToV6WithoutInferringCapabilities(t *testing.T) {
	cfg, err := Decode(strings.NewReader(openAIVersionedTOML(5, `"hosted_web_search", "text", "tools"`, false)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 6 {
		t.Fatalf("schema version = %d, want 6", cfg.SchemaVersion)
	}
	want := []string{"hosted_web_search", "text", "tools"}
	if !slices.Equal(cfg.Providers[0].Models[0].Capabilities, want) {
		t.Fatalf("capabilities = %v, want %v", cfg.Providers[0].Models[0].Capabilities, want)
	}
}

func TestSchemaV5CodexModelMigratesToWrittenBaseline(t *testing.T) {
	cfg, err := Decode(strings.NewReader(openAIVersionedTOML(5, `"text", "tools"`, true)))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"}
	if !slices.Equal(cfg.Providers[0].Models[0].Capabilities, want) {
		t.Fatalf("capabilities = %v, want %v", cfg.Providers[0].Models[0].Capabilities, want)
	}
}

func TestPreV6SchemasRejectCustomToolsCapability(t *testing.T) {
	for _, version := range []int{3, 4, 5} {
		t.Run(fmt.Sprintf("schema-%d", version), func(t *testing.T) {
			_, err := Decode(strings.NewReader(openAIVersionedTOML(version, `"custom_tools", "text", "tools"`, false)))
			var configErr *Error
			if !errors.As(err, &configErr) || configErr.Code != CodeValidationFailed || configErr.Path != "providers[0].models[0].capabilities[0]" {
				t.Fatalf("error = %#v, want pre-v6 custom_tools rejection", err)
			}
		})
	}
}

func TestSchemaV6AcceptsCompleteCodexBaseline(t *testing.T) {
	cfg, err := Decode(strings.NewReader(openAIVersionedTOML(6, `"custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"`, true)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 6 || cfg.Providers[0].Models[0].Codex == nil {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestSchemaV6CanonicalRoundTrip(t *testing.T) {
	cfg, err := Decode(strings.NewReader(openAIVersionedTOML(6, `"custom_tools", "hosted_web_search", "namespace_tools", "parallel_tools", "text", "tools"`, true)))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Canonical(cfg)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	encodedAgain, err := Canonical(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedAgain, encoded) {
		t.Fatal("canonical schema-v6 encoding changed after round trip")
	}
}
