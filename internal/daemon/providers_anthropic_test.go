package daemon

import (
	"context"
	"testing"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/credential"
)

func anthropicRegistryConfig(enabled bool) config.Config {
	configuration := config.Default()
	configuration.Providers = []config.ProviderConfig{
		{ID: "mock", Kind: config.ProviderKindMock, Enabled: true},
		{
			ID: "anthropic-primary", Kind: config.ProviderKindAnthropic, Enabled: enabled,
			Credential: &config.CredentialRef{Source: config.CredentialSourceEnv, Name: "ANTHROPIC_API_KEY"},
			Models:     []config.ProviderModelConfig{{Alias: "anthropic/claude-test", Upstream: "claude-test", Capabilities: []string{"image_input", "structured_output", "text", "tools"}, DefaultMaxOutputTokens: 1024, MaxOutputTokens: 4096}},
		},
	}
	return configuration
}

func TestBuildProviderRuntimeOmitsMissingAnthropicCredential(t *testing.T) {
	runtime, err := buildProviderRuntime(anthropicRegistryConfig(true), credential.NewEnvSource(func(string) (string, bool) { return "", false }))
	if err != nil {
		t.Fatal(err)
	}
	models, err := runtime.registry.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 8 {
		t.Fatalf("models = %#v", models)
	}
	statuses, err := runtime.summaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].ID != "anthropic-primary" || statuses[0].Kind != "anthropic" || statuses[0].Available || statuses[0].Code != "authentication_failed" || statuses[0].Reference != "ANTHROPIC_API_KEY" || statuses[1].ID != "mock" || !statuses[1].Available {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestBuildProviderRuntimeExposesAvailableAnthropicAlias(t *testing.T) {
	runtime, err := buildProviderRuntime(anthropicRegistryConfig(true), credential.NewEnvSource(func(name string) (string, bool) { return "private-key", name == "ANTHROPIC_API_KEY" }))
	if err != nil {
		t.Fatal(err)
	}
	models, err := runtime.registry.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 9 || models[0].ID() != "anthropic/claude-test" || models[0].Provider() != "anthropic-primary" {
		t.Fatalf("models = %#v", models)
	}
	statuses, err := runtime.summaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].ID != "anthropic-primary" || !statuses[0].Available || statuses[0].Reference != "" || statuses[1].ID != "mock" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestBuildProviderRuntimeExcludesDisabledAnthropicProvider(t *testing.T) {
	runtime, err := buildProviderRuntime(anthropicRegistryConfig(false), credential.NewEnvSource(func(string) (string, bool) { return "private-key", true }))
	if err != nil {
		t.Fatal(err)
	}
	models, err := runtime.registry.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := runtime.summaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 8 || len(statuses) != 1 || statuses[0].ID != "mock" {
		t.Fatalf("models=%#v statuses=%#v", models, statuses)
	}
}
