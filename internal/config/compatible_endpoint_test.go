package config

import (
	"errors"
	"strings"
	"testing"
)

func compatibleProvider(baseURL, wire string) ProviderConfig {
	provider := validOpenAIProvider("deepseek", "deepseek/chat-test")
	provider.BaseURL = baseURL
	if wire != "" {
		provider.Wire = ProviderWire(wire)
	}
	return provider
}

func TestDecodeSchemaV7CompatibleEndpoint(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`schema_version = 7
[[providers]]
id = "deepseek"
kind = "openai"
enabled = true
base_url = "https://api.deepseek.com/v1"
wire = "chat"
[providers.credential]
source = "env"
name = "OPENAI_DEEPSEEK_KEY"
[[providers.models]]
alias = "deepseek/chat-test"
upstream = "deepseek-chat"
capabilities = ["text", "tools", "parallel_tools"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("providers = %#v", cfg.Providers)
	}
	provider := cfg.Providers[0]
	if provider.BaseURL != "https://api.deepseek.com/v1" || provider.Wire != ProviderWireChat {
		t.Fatalf("endpoint = %q wire = %q", provider.BaseURL, provider.Wire)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	if _, err := Canonical(cfg); err != nil {
		t.Fatalf("Canonical() failed: %v", err)
	}
}

func TestDecodeOlderSchemaRejectsEndpointFields(t *testing.T) {
	for _, version := range []string{"3", "4", "5", "6"} {
		for _, field := range []string{"base_url = \"https://api.deepseek.com/v1\"", "wire = \"chat\""} {
			document := "schema_version = " + version + "\n[[providers]]\nid = \"deepseek\"\nkind = \"openai\"\nenabled = true\n" + field + "\n[providers.credential]\nsource = \"env\"\nname = \"OPENAI_API_KEY\"\n[[providers.models]]\nalias = \"a\"\nupstream = \"u\"\ncapabilities = [\"text\"]\n"
			if _, err := Decode(strings.NewReader(document)); err == nil {
				t.Fatalf("v%s accepted %q", version, field)
			}
		}
	}
}

func TestValidateChatWireRejectsDeferredCapabilities(t *testing.T) {
	for _, capability := range []string{"continuation", "image_input"} {
		cfg := Default()
		provider := compatibleProvider("https://api.deepseek.com/v1", "chat")
		provider.Models[0].Capabilities = []string{"text", capability}
		cfg.Providers = []ProviderConfig{provider}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("chat wire accepted capability %q", capability)
		}
	}
}

func TestValidateCompatibleEndpointRules(t *testing.T) {
	for _, tc := range []struct {
		name       string
		baseURL    string
		wire       string
		kind       ProviderKind
		caps       []string
		codex      bool
		shouldPass bool
	}{
		{"https endpoint", "https://api.deepseek.com/v1", "chat", ProviderKindOpenAI, []string{"text", "tools"}, false, true},
		{"loopback http", "http://127.0.0.1:8080/v1", "chat", ProviderKindOpenAI, []string{"text"}, false, true},
		{"default wire is responses", "https://internal.example/v1", "", ProviderKindOpenAI, []string{"text"}, false, true},
		{"plain http rejected", "http://api.example.com/v1", "chat", ProviderKindOpenAI, []string{"text"}, false, false},
		{"userinfo rejected", "https://user:pass@api.example.com/v1", "chat", ProviderKindOpenAI, []string{"text"}, false, false},
		{"query rejected", "https://api.example.com/v1?key=x", "chat", ProviderKindOpenAI, []string{"text"}, false, false},
		{"fragment rejected", "https://api.example.com/v1#frag", "chat", ProviderKindOpenAI, []string{"text"}, false, false},
		{"unknown wire", "https://api.example.com/v1", "grpc", ProviderKindOpenAI, []string{"text"}, false, false},
		{"base_url on anthropic", "https://api.example.com", "", ProviderKindAnthropic, nil, false, false},
		{"wire on mock", "https://api.example.com/v1", "chat", ProviderKindMock, nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			switch tc.kind {
			case ProviderKindOpenAI:
				cfg.Providers = []ProviderConfig{compatibleProvider(tc.baseURL, tc.wire)}
			case ProviderKindAnthropic:
				provider := validAnthropicProvider("anthropic", "anthropic/model")
				provider.BaseURL = tc.baseURL
				cfg.Providers = []ProviderConfig{provider}
			case ProviderKindMock:
				cfg.Providers = []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true, BaseURL: tc.baseURL, Wire: ProviderWire(tc.wire)}}
			}
			if tc.caps != nil {
				cfg.Providers[0].Models[0].Capabilities = tc.caps
			}
			err := cfg.Validate()
			if tc.shouldPass && err != nil {
				t.Fatalf("Validate() = %v, want pass", err)
			}
			if !tc.shouldPass {
				if err == nil {
					t.Fatal("Validate() accepted invalid endpoint configuration")
				}
				// For "wire on mock" case, verify the error path
				if tc.name == "wire on mock" {
					var configErr *Error
					if !errors.As(err, &configErr) || configErr.Path != "providers[0].wire" {
						t.Fatalf("wire on mock: error path = %q, want %q", configErr.Path, "providers[0].wire")
					}
				}
			}
		})
	}
}

func TestValidateChatWireRestrictions(t *testing.T) {
	for _, capability := range []string{"custom_tools", "namespace_tools", "hosted_web_search"} {
		cfg := Default()
		provider := compatibleProvider("https://api.deepseek.com/v1", "chat")
		provider.Models[0].Capabilities = []string{"text", "tools", capability}
		cfg.Providers = []ProviderConfig{provider}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("chat wire accepted capability %q", capability)
		}
	}
	cfg := Default()
	provider := compatibleProvider("https://api.deepseek.com/v1", "chat")
	provider.Models[0].Capabilities = []string{"text", "tools"}
	provider.Models[0].Codex = &CodexModelConfig{Enabled: true}
	cfg.Providers = []ProviderConfig{provider}
	if err := cfg.Validate(); err == nil {
		t.Fatal("chat wire accepted a Codex-enabled model")
	}
}

func TestCanonicalPreservesEndpointRoundTrip(t *testing.T) {
	cfg := Default()
	cfg.Providers = []ProviderConfig{compatibleProvider("https://api.deepseek.com/v1", "chat")}
	encoded, err := Canonical(cfg)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatalf("redecode failed: %v", err)
	}
	if len(decoded.Providers) != 1 || decoded.Providers[0].BaseURL != "https://api.deepseek.com/v1" || decoded.Providers[0].Wire != ProviderWireChat {
		t.Fatalf("round trip = %#v", decoded.Providers)
	}
	again, err := Canonical(decoded)
	if err != nil || string(again) != string(encoded) {
		t.Fatalf("canonical encoding is not stable: %q vs %q", again, encoded)
	}
}
