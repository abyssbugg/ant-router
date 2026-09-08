package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/credential"
)

func codexProviderRegistryConfig() config.Config {
	configuration := providerRegistryConfig()
	metadata := func(name string, priority int) *config.CodexModelConfig {
		return &config.CodexModelConfig{Enabled: true, DisplayName: name, Description: name + " routed through Ant Router", ContextWindow: 32768, EffectiveContextWindowPercent: 90, Priority: priority, Visibility: config.CodexVisibilityList}
	}
	configuration.Providers[0].Models = []config.ProviderModelConfig{{Alias: "mock/text", Codex: metadata("Mock Text", 10)}}
	configuration.Providers[1].Models[0].Codex = metadata("OpenAI Test", 20)
	return configuration
}

func TestCodexCatalogOmitsUnavailableProviderModels(t *testing.T) {
	runtime, err := buildProviderRuntime(codexProviderRegistryConfig(), credential.NewEnvSource(func(string) (string, bool) { return "", false }))
	if err != nil {
		t.Fatal(err)
	}
	runtime.configGeneration = 7
	catalog, err := runtime.codexCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body := string(catalog.JSON)
	if catalog.Generation != 7 || !strings.Contains(body, `"slug": "mock/text"`) || strings.Contains(body, "openai/gpt-test") {
		t.Fatalf("generation=%d catalog=%s", catalog.Generation, body)
	}
}

func TestCodexCatalogIncludesAvailableProviderModels(t *testing.T) {
	runtime, err := buildProviderRuntime(codexProviderRegistryConfig(), credential.NewEnvSource(func(name string) (string, bool) { return "private-key", name == "OPENAI_API_KEY" }))
	if err != nil {
		t.Fatal(err)
	}
	runtime.configGeneration = 11
	catalog, err := runtime.codexCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body := string(catalog.JSON)
	if catalog.Generation != 11 || !strings.Contains(body, `"slug": "openai/gpt-test"`) || !strings.Contains(body, `"slug": "mock/text"`) {
		t.Fatalf("generation=%d catalog=%s", catalog.Generation, body)
	}
}
