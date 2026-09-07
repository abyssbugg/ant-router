package config

import (
	"bytes"
	"testing"
)

func TestMockProviderAcceptsExplicitCodexMetadataForKnownModels(t *testing.T) {
	metadata := validCodexMetadata()
	configuration := Default()
	configuration.Providers = []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true, Models: []ProviderModelConfig{{Alias: "mock/text", Codex: metadata}}}}
	if err := configuration.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := Canonical(configuration)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Providers) != 1 || len(decoded.Providers[0].Models) != 1 || decoded.Providers[0].Models[0].Codex == nil || !decoded.Providers[0].Models[0].Codex.Enabled {
		t.Fatalf("decoded = %#v", decoded.Providers)
	}
}

func TestMockProviderRejectsUnknownOrRuntimeModelOverrides(t *testing.T) {
	metadata := validCodexMetadata()
	tests := []ProviderModelConfig{
		{Alias: "mock/unknown", Codex: metadata},
		{Alias: "mock/text", Upstream: "changed", Codex: metadata},
		{Alias: "mock/text", Capabilities: []string{"text"}, Codex: metadata},
		{Alias: "mock/text"},
	}
	for _, model := range tests {
		configuration := Default()
		configuration.Providers = []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true, Models: []ProviderModelConfig{model}}}
		if err := configuration.Validate(); err == nil {
			t.Fatalf("accepted %#v", model)
		}
	}
}
