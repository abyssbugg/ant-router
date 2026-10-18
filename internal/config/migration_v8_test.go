package config

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// v8ProviderTOML renders one OpenAI-kind provider with extra provider-level
// fields and an optional credential block (empty string omits it).
func v8ProviderTOML(version int, extra, credentialBlock string) string {
	return fmt.Sprintf(`schema_version = %d
[[providers]]
id = "compat"
kind = "openai"
enabled = true
%s
%s
[[providers.models]]
alias = "compat/model"
upstream = "model-1"
capabilities = ["text", "tools"]
`, version, extra, credentialBlock)
}

func credentialTOML(name string) string {
	return fmt.Sprintf("[providers.credential]\nsource = \"env\"\nname = %q\n", name)
}

func poolTOML(names ...string) string {
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "[[providers.credentials]]\nsource = \"env\"\nname = %q\n", name)
	}
	return b.String()
}

func decodeAndValidate(t *testing.T, document string) (Config, error) {
	t.Helper()
	cfg, err := Decode(strings.NewReader(document))
	if err != nil {
		return Config{}, err
	}
	return cfg, cfg.Validate()
}

func requireConfigErrorPath(t *testing.T, err error, path string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error at %s, got nil", path)
	}
	code, ok := CodeOf(err)
	if !ok {
		t.Fatalf("error %v has no config code", err)
	}
	var cfgErr *Error
	if !errors.As(err, &cfgErr) || cfgErr.Path != path {
		t.Fatalf("error = %v (code %s), want path %s", err, code, path)
	}
}

func TestSchemaV7MigratesToV8(t *testing.T) {
	cfg, err := decodeAndValidate(t, v8ProviderTOML(7, "", credentialTOML("OPENAI_API_KEY")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 8 || SchemaVersion != 8 {
		t.Fatalf("schema version = %d (current %d), want 8", cfg.SchemaVersion, SchemaVersion)
	}
}

func TestSchemaV8FieldsRejectedBeforeV8(t *testing.T) {
	for _, tc := range []struct{ extra, credentials, path string }{
		{`preset = "deepseek"`, credentialTOML("DEEPSEEK_API_KEY"), "providers[0].preset"},
		{`proxy = "environment"`, credentialTOML("OPENAI_API_KEY"), "providers[0].proxy"},
		{"", poolTOML("OPENAI_KEY_A", "OPENAI_KEY_B"), "providers[0].credentials"},
	} {
		_, err := Decode(strings.NewReader(v8ProviderTOML(7, tc.extra, tc.credentials)))
		if code, _ := CodeOf(err); code != CodeUnknownField {
			t.Fatalf("%s in v7: err = %v, want %s", tc.path, err, CodeUnknownField)
		}
		requireConfigErrorPath(t, err, tc.path)
	}
}

func TestPresetSuppliesEndpointDefaults(t *testing.T) {
	for preset, want := range map[string]struct{ baseURL, credential string }{
		"deepseek":   {"https://api.deepseek.com/v1", "DEEPSEEK_API_KEY"},
		"moonshot":   {"https://api.moonshot.ai/v1", "MOONSHOT_API_KEY"},
		"zhipu":      {"https://open.bigmodel.cn/api/paas/v4", "ZHIPU_API_KEY"},
		"qwen":       {"https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "DASHSCOPE_API_KEY"},
		"xai":        {"https://api.x.ai/v1", "XAI_API_KEY"},
		"openrouter": {"https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"},
		"ollama":     {"http://127.0.0.1:11434/v1", "OLLAMA_API_KEY"},
	} {
		cfg, err := decodeAndValidate(t, v8ProviderTOML(8, fmt.Sprintf("preset = %q", preset), credentialTOML(want.credential)))
		if err != nil {
			t.Fatalf("preset %s: %v", preset, err)
		}
		endpoint := cfg.Providers[0].Endpoint()
		if endpoint.BaseURL != want.baseURL || endpoint.Wire != ProviderWireChat {
			t.Fatalf("preset %s endpoint = %+v, want %s chat", preset, endpoint, want.baseURL)
		}
		// Presets also accept the generic OPENAI_ prefix.
		if _, err := decodeAndValidate(t, v8ProviderTOML(8, fmt.Sprintf("preset = %q", preset), credentialTOML("OPENAI_COMPAT_KEY"))); err != nil {
			t.Fatalf("preset %s with OPENAI_ credential: %v", preset, err)
		}
	}
}

func TestPresetValidation(t *testing.T) {
	_, err := decodeAndValidate(t, v8ProviderTOML(8, `preset = "nope"`, credentialTOML("OPENAI_API_KEY")))
	requireConfigErrorPath(t, err, "providers[0].preset")

	// A preset scopes credentials to its provider; another vendor's key
	// name must not be shipped to the preset endpoint.
	_, err = decodeAndValidate(t, v8ProviderTOML(8, `preset = "deepseek"`, credentialTOML("XAI_API_KEY")))
	requireConfigErrorPath(t, err, "providers[0].credential.name")

	// An explicit base_url overrides the preset endpoint, so only the
	// generic OPENAI_ prefix is accepted.
	cfg, err := decodeAndValidate(t, v8ProviderTOML(8, "preset = \"deepseek\"\nbase_url = \"https://proxy.example.com/v1\"", credentialTOML("OPENAI_COMPAT_KEY")))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers[0].Endpoint(); got.BaseURL != "https://proxy.example.com/v1" || got.Wire != ProviderWireChat {
		t.Fatalf("endpoint = %+v", got)
	}
	_, err = decodeAndValidate(t, v8ProviderTOML(8, "preset = \"deepseek\"\nbase_url = \"https://proxy.example.com/v1\"", credentialTOML("DEEPSEEK_API_KEY")))
	requireConfigErrorPath(t, err, "providers[0].credential.name")

	cfg, err = decodeAndValidate(t, v8ProviderTOML(8, "preset = \"xai\"\nwire = \"responses\"", credentialTOML("XAI_API_KEY")))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers[0].Endpoint(); got.Wire != ProviderWireResponses {
		t.Fatalf("explicit wire not honored: %+v", got)
	}

	anthropic := strings.Replace(v8ProviderTOML(8, `preset = "deepseek"`, credentialTOML("ANTHROPIC_API_KEY")), `kind = "openai"`, `kind = "anthropic"`, 1)
	_, err = decodeAndValidate(t, anthropic)
	requireConfigErrorPath(t, err, "providers[0].preset")
}

func TestEndpointWithoutPresetPreservesV7Behavior(t *testing.T) {
	cfg, err := decodeAndValidate(t, v8ProviderTOML(8, "", credentialTOML("OPENAI_API_KEY")))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers[0].Endpoint(); got.BaseURL != "" || got.Wire != ProviderWireResponses {
		t.Fatalf("default endpoint = %+v, want official responses", got)
	}
}

func TestProxyValidation(t *testing.T) {
	for _, valid := range []string{"environment", "http://127.0.0.1:3128", "https://proxy.example.com:8443", "socks5://127.0.0.1:1080"} {
		cfg, err := decodeAndValidate(t, v8ProviderTOML(8, fmt.Sprintf("proxy = %q", valid), credentialTOML("OPENAI_API_KEY")))
		if err != nil {
			t.Fatalf("proxy %q: %v", valid, err)
		}
		if cfg.Providers[0].Proxy != valid {
			t.Fatalf("proxy = %q, want %q", cfg.Providers[0].Proxy, valid)
		}
	}
	for _, invalid := range []string{"env", "ftp://proxy.example.com", "http://user:secret@proxy.example.com:3128", "http://proxy.example.com/path", "http://proxy.example.com?x=1", "http://", "socks5h://127.0.0.1:1080"} {
		_, err := decodeAndValidate(t, v8ProviderTOML(8, fmt.Sprintf("proxy = %q", invalid), credentialTOML("OPENAI_API_KEY")))
		requireConfigErrorPath(t, err, "providers[0].proxy")
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Fatalf("proxy error leaks credentials: %v", err)
		}
	}
	mock := "schema_version = 8\n[[providers]]\nid = \"mock\"\nkind = \"mock\"\nenabled = true\nproxy = \"environment\"\n"
	_, err := decodeAndValidate(t, mock)
	requireConfigErrorPath(t, err, "providers[0].proxy")
}

func TestCredentialPoolValidation(t *testing.T) {
	cfg, err := decodeAndValidate(t, v8ProviderTOML(8, `preset = "deepseek"`, poolTOML("DEEPSEEK_KEY_A", "DEEPSEEK_KEY_B")))
	if err != nil {
		t.Fatal(err)
	}
	refs := cfg.Providers[0].CredentialRefs()
	if len(refs) != 2 || refs[0].Name != "DEEPSEEK_KEY_A" || refs[1].Name != "DEEPSEEK_KEY_B" {
		t.Fatalf("CredentialRefs() = %+v", refs)
	}
	single, err := decodeAndValidate(t, v8ProviderTOML(8, "", credentialTOML("OPENAI_API_KEY")))
	if err != nil {
		t.Fatal(err)
	}
	if refs := single.Providers[0].CredentialRefs(); len(refs) != 1 || refs[0].Name != "OPENAI_API_KEY" {
		t.Fatalf("single CredentialRefs() = %+v", refs)
	}

	_, err = decodeAndValidate(t, v8ProviderTOML(8, "", credentialTOML("OPENAI_API_KEY")+poolTOML("OPENAI_KEY_A", "OPENAI_KEY_B")))
	requireConfigErrorPath(t, err, "providers[0].credentials")
	_, err = decodeAndValidate(t, v8ProviderTOML(8, "", poolTOML("OPENAI_KEY_A")))
	requireConfigErrorPath(t, err, "providers[0].credentials")
	_, err = decodeAndValidate(t, v8ProviderTOML(8, "", poolTOML("OPENAI_KEY_A", "OPENAI_KEY_A")))
	requireConfigErrorPath(t, err, "providers[0].credentials[1].name")
	_, err = decodeAndValidate(t, v8ProviderTOML(8, `preset = "deepseek"`, poolTOML("DEEPSEEK_KEY_A", "XAI_KEY_B")))
	requireConfigErrorPath(t, err, "providers[0].credentials[1].name")
	names := make([]string, MaxCredentialsPerProvider+1)
	for i := range names {
		names[i] = fmt.Sprintf("OPENAI_KEY_%d", i)
	}
	_, err = decodeAndValidate(t, v8ProviderTOML(8, "", poolTOML(names...)))
	requireConfigErrorPath(t, err, "providers[0].credentials")
}

func TestSchemaV8CanonicalRoundTripAndClone(t *testing.T) {
	cfg, err := decodeAndValidate(t, v8ProviderTOML(8, "preset = \"openrouter\"\nproxy = \"socks5://127.0.0.1:1080\"", poolTOML("OPENROUTER_KEY_A", "OPENROUTER_KEY_B")))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Canonical(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"preset = 'openrouter'", "proxy = 'socks5://127.0.0.1:1080'", "[[providers.credentials]]"} {
		if !bytes.Contains(encoded, []byte(want)) {
			t.Fatalf("canonical output missing %q:\n%s", want, encoded)
		}
	}
	again, err := decodeAndValidate(t, string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := Canonical(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("canonical round trip changed:\n%s\n---\n%s", encoded, reencoded)
	}
	cloned := cfg.clone()
	cloned.Providers[0].Credentials[0].Name = "MUTATED"
	if cfg.Providers[0].Credentials[0].Name != "OPENROUTER_KEY_A" {
		t.Fatal("clone shares the credential pool")
	}
}
