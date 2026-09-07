package daemon

import (
	"context"
	"testing"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/credential"
)

func providerRegistryConfig() config.Config {
	configuration := config.Default()
	configuration.Providers = []config.ProviderConfig{
		{ID: "mock", Kind: config.ProviderKindMock, Enabled: true},
		{
			ID:      "openai-primary",
			Kind:    config.ProviderKindOpenAI,
			Enabled: true,
			Credential: &config.CredentialRef{
				Source: config.CredentialSourceEnv,
				Name:   "OPENAI_API_KEY",
			},
			Models: []config.ProviderModelConfig{{Alias: "openai/gpt-test", Upstream: "gpt-test", Capabilities: []string{"text"}}},
		},
		{
			ID:      "openai-disabled",
			Kind:    config.ProviderKindOpenAI,
			Enabled: false,
			Credential: &config.CredentialRef{
				Source: config.CredentialSourceEnv,
				Name:   "DISABLED_KEY",
			},
			Models: []config.ProviderModelConfig{{Alias: "openai/disabled", Upstream: "disabled", Capabilities: []string{"text"}}},
		},
	}
	return configuration
}

func TestBuildProviderRegistryOmitsUnavailableAndDisabledModels(t *testing.T) {
	t.Parallel()
	configuration := providerRegistryConfig()
	source := credential.NewEnvSource(func(string) (string, bool) { return "", false })
	runtime, err := buildProviderRuntime(configuration, source)
	if err != nil {
		t.Fatal(err)
	}
	models, err := runtime.registry.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 8 || models[0].ID() != "mock/codex" || models[7].ID() != "mock/vision" {
		t.Fatalf("models = %#v", models)
	}
	statuses, err := runtime.summaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].ID != "mock" || !statuses[0].Available {
		t.Fatalf("statuses = %#v", statuses)
	}
	openAI := statuses[1]
	if openAI.ID != "openai-primary" || openAI.Kind != "openai" || openAI.Available || openAI.Code != "authentication_failed" || openAI.Reference != "OPENAI_API_KEY" {
		t.Fatalf("OpenAI status = %#v", openAI)
	}
}

func TestBuildProviderRegistryExposesAvailableOpenAIAliases(t *testing.T) {
	t.Parallel()
	configuration := providerRegistryConfig()
	source := credential.NewEnvSource(func(name string) (string, bool) {
		if name == "OPENAI_API_KEY" {
			return "private-test-key", true
		}
		return "", false
	})
	runtime, err := buildProviderRuntime(configuration, source)
	if err != nil {
		t.Fatal(err)
	}
	models, err := runtime.registry.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 9 || models[8].ID() != "openai/gpt-test" || models[8].Provider() != "openai-primary" {
		t.Fatalf("models = %#v", models)
	}
	statuses, err := runtime.summaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || !statuses[1].Available || statuses[1].Reference != "" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestBuildProviderRegistryRejectsNilCredentialSource(t *testing.T) {
	t.Parallel()
	var typedNil *credential.EnvSource
	for _, source := range []credential.Source{nil, typedNil} {
		if runtime, err := buildProviderRuntime(providerRegistryConfig(), source); err == nil || runtime != nil {
			t.Fatalf("buildProviderRuntime() = %#v, %v", runtime, err)
		}
	}
}
