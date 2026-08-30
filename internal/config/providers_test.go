package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func validOpenAIProvider(id, alias string) ProviderConfig {
	return ProviderConfig{
		ID:      id,
		Kind:    ProviderKindOpenAI,
		Enabled: true,
		Credential: &CredentialRef{
			Source: CredentialSourceEnv,
			Name:   "OPENAI_API_KEY",
		},
		Models: []ProviderModelConfig{{
			Alias:        alias,
			Upstream:     "gpt-test",
			Capabilities: []string{"text", "tools"},
		}},
	}
}

func TestDefaultUsesSchemaV3WithoutProviders(t *testing.T) {
	cfg := Default()
	if cfg.SchemaVersion != 3 {
		t.Fatalf("schema version = %d, want 3", cfg.SchemaVersion)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("providers = %#v, want none", cfg.Providers)
	}
}

func TestDecodeSchemaV3OpenAIProvider(t *testing.T) {
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
capabilities = ["text", "tools"]
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(cfg.Providers) != 1 || !reflect.DeepEqual(cfg.Providers[0], validOpenAIProvider("openai-primary", "openai/gpt-test")) {
		t.Fatalf("providers = %#v", cfg.Providers)
	}
}

func TestProviderValidation(t *testing.T) {
	valid := Default()
	valid.DataPlane.Enabled = true
	valid.Providers = []ProviderConfig{validOpenAIProvider("openai-primary", "openai/gpt-test")}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	tests := []struct {
		name string
		edit func(*Config)
		path string
	}{
		{"invalid provider id", func(c *Config) { c.Providers[0].ID = "OpenAI" }, "providers[0].id"},
		{"missing credential", func(c *Config) { c.Providers[0].Credential = nil }, "providers[0].credential"},
		{"unsupported credential source", func(c *Config) { c.Providers[0].Credential.Source = "file" }, "providers[0].credential.source"},
		{"invalid environment name", func(c *Config) { c.Providers[0].Credential.Name = "openai-key" }, "providers[0].credential.name"},
		{"missing models", func(c *Config) { c.Providers[0].Models = nil }, "providers[0].models"},
		{"invalid alias", func(c *Config) { c.Providers[0].Models[0].Alias = "../secret" }, "providers[0].models[0].alias"},
		{"missing upstream", func(c *Config) { c.Providers[0].Models[0].Upstream = "" }, "providers[0].models[0].upstream"},
		{"unknown capability", func(c *Config) { c.Providers[0].Models[0].Capabilities = []string{"unknown"} }, "providers[0].models[0].capabilities[0]"},
		{"duplicate capability", func(c *Config) { c.Providers[0].Models[0].Capabilities = []string{"text", "text"} }, "providers[0].models[0].capabilities[1]"},
		{"duplicate provider id", func(c *Config) {
			c.Providers = append(c.Providers, validOpenAIProvider("openai-primary", "openai/other"))
		}, "providers[1].id"},
		{"duplicate alias", func(c *Config) { c.Providers = append(c.Providers, validOpenAIProvider("other", "openai/gpt-test")) }, "providers[1].models[0].alias"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid.clone()
			tt.edit(&cfg)
			err := cfg.Validate()
			var configErr *Error
			if !errors.As(err, &configErr) || configErr.Path != tt.path {
				t.Fatalf("Validate() error = %v, want path %q", err, tt.path)
			}
		})
	}
}

func TestMockProviderRejectsCredentialAndModels(t *testing.T) {
	for _, provider := range []ProviderConfig{
		{ID: "mock", Kind: ProviderKindMock, Enabled: true, Credential: &CredentialRef{Source: CredentialSourceEnv, Name: "MOCK_KEY"}},
		{ID: "mock", Kind: ProviderKindMock, Enabled: true, Models: []ProviderModelConfig{{Alias: "mock/custom", Upstream: "custom", Capabilities: []string{"text"}}}},
	} {
		cfg := Default()
		cfg.Providers = []ProviderConfig{provider}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate() accepted mock provider %#v", provider)
		}
	}
}

func TestCanonicalProviderOrderIsSemantic(t *testing.T) {
	a := Default()
	a.Providers = []ProviderConfig{
		validOpenAIProvider("zeta", "zeta/model"),
		validOpenAIProvider("alpha", "alpha/model"),
	}
	a.Providers[0].Models[0].Capabilities = []string{"tools", "text"}
	b := a.clone()
	b.Providers[0], b.Providers[1] = b.Providers[1], b.Providers[0]
	b.Providers[1].Models[0].Capabilities = []string{"text", "tools"}

	canonicalA, err := Canonical(a)
	if err != nil {
		t.Fatal(err)
	}
	canonicalB, err := Canonical(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonicalA) != string(canonicalB) {
		t.Fatalf("provider order changed canonical output:\nA=%s\nB=%s", canonicalA, canonicalB)
	}
}

func TestConfigCloneDeepCopiesProviders(t *testing.T) {
	original := Default()
	original.Providers = []ProviderConfig{validOpenAIProvider("openai-primary", "openai/gpt-test")}
	clone := original.clone()

	clone.Providers[0].ID = "changed"
	clone.Providers[0].Credential.Name = "CHANGED_KEY"
	clone.Providers[0].Models[0].Alias = "changed/model"
	clone.Providers[0].Models[0].Capabilities[0] = "image_input"

	want := validOpenAIProvider("openai-primary", "openai/gpt-test")
	if !reflect.DeepEqual(original.Providers[0], want) {
		t.Fatalf("clone mutation changed original: %#v", original.Providers[0])
	}
}

func TestSnapshotProviderConfigurationIsImmutable(t *testing.T) {
	cfg := Default()
	cfg.Providers = []ProviderConfig{validOpenAIProvider("openai-primary", "openai/gpt-test")}
	snapshot := Snapshot{config: cfg.clone()}
	got := snapshot.Config()
	got.Providers[0].Credential.Name = "CHANGED_KEY"
	got.Providers[0].Models[0].Capabilities[0] = "image_input"
	fresh := snapshot.Config()
	if fresh.Providers[0].Credential.Name != "OPENAI_API_KEY" || fresh.Providers[0].Models[0].Capabilities[0] != "text" {
		t.Fatalf("snapshot configuration was mutated: %#v", fresh.Providers[0])
	}
}

func TestCandidateProviderConfigurationIsImmutable(t *testing.T) {
	cfg := Default()
	cfg.Providers = []ProviderConfig{validOpenAIProvider("openai-primary", "openai/gpt-test")}
	candidate, err := NewCandidate(cfg, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers[0].ID = "changed-input"
	got := candidate.Config()
	got.Providers[0].ID = "changed-output"
	if candidate.Config().Providers[0].ID != "openai-primary" {
		t.Fatalf("candidate provider changed to %q", candidate.Config().Providers[0].ID)
	}
}

func TestProviderRegistryBoundsAndReservedAliases(t *testing.T) {
	t.Run("provider limit", func(t *testing.T) {
		cfg := Default()
		for i := 0; i <= MaxProviders; i++ {
			cfg.Providers = append(cfg.Providers, validOpenAIProvider(fmt.Sprintf("provider-%d", i), fmt.Sprintf("provider-%d/model", i)))
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate() accepted too many providers")
		}
	})
	t.Run("model limit", func(t *testing.T) {
		cfg := Default()
		provider := validOpenAIProvider("openai", "openai/model-0")
		for i := 1; i <= MaxModelsPerProvider; i++ {
			provider.Models = append(provider.Models, ProviderModelConfig{Alias: fmt.Sprintf("openai/model-%d", i), Upstream: fmt.Sprintf("model-%d", i), Capabilities: []string{"text"}})
		}
		cfg.Providers = []ProviderConfig{provider}
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate() accepted too many models")
		}
	})
	t.Run("mock alias reserved", func(t *testing.T) {
		cfg := Default()
		cfg.Providers = []ProviderConfig{
			{ID: "mock", Kind: ProviderKindMock, Enabled: true},
			validOpenAIProvider("openai", "mock/text"),
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate() accepted alias reserved by fixed mock catalog")
		}
	})
}

func TestOpenAIProviderCapabilityConstraints(t *testing.T) {
	tests := []struct {
		name         string
		capabilities []string
	}{
		{"continuation deferred", []string{"continuation", "text"}},
		{"parallel tools requires tools", []string{"parallel_tools", "text"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			provider := validOpenAIProvider("openai", "openai/model")
			provider.Models[0].Capabilities = tt.capabilities
			cfg.Providers = []ProviderConfig{provider}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() accepted capabilities %v", tt.capabilities)
			}
		})
	}
}

func TestDataPlaneRequiresAnyEnabledProvider(t *testing.T) {
	cfg := Default()
	cfg.DataPlane.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted data plane without enabled provider")
	}
	cfg.Providers = []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with mock provider error = %v", err)
	}
}
