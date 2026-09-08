package codexprofile

import (
	"bytes"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func testAbsolutePath(value string) string {
	if runtime.GOOS != "windows" {
		return value
	}
	return filepath.Join(`C:\`, filepath.FromSlash(strings.TrimPrefix(value, "/")))
}

func validOptions() Options {
	return Options{BaseURL: "http://127.0.0.1:8787/v1", CatalogPath: testAbsolutePath("/private/state/models.json"), RouterctlPath: testAbsolutePath("/usr/local/bin/routerctl"), Instance: "default", StateRoot: testAbsolutePath("/private/state"), RuntimeRoot: testAbsolutePath("/private/run")}
}

func TestRenderProducesDeterministicOfficialProfile(t *testing.T) {
	options := validOptions()
	first, err := Render(options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("profile is not deterministic")
	}
	var decoded map[string]any
	if err := toml.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("invalid TOML: %v", err)
	}
	if decoded["model_provider"] != "ant-router" || decoded["model_catalog_json"] != options.CatalogPath {
		t.Fatalf("profile = %#v", decoded)
	}
	text := string(first)
	for _, required := range []string{"[model_providers.ant-router]", "wire_api = 'responses'", "request_max_retries = 0", "stream_max_retries = 0", "[model_providers.ant-router.auth]", "'codex', 'token'"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing %q in %s", required, text)
		}
	}
	for _, forbidden := range []string{"bearer", "api_key", "experimental_bearer_token", "env_key"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("profile contains %q", forbidden)
		}
	}
	if !strings.Contains(text, options.RouterctlPath) {
		t.Fatalf("profile does not contain command path")
	}
}

func TestRenderRejectsInvalidPorts(t *testing.T) {
	for _, baseURL := range []string{"http://127.0.0.1:0/v1", "http://127.0.0.1:65536/v1", "http://127.0.0.1:not-a-port/v1"} {
		options := validOptions()
		options.BaseURL = baseURL
		if _, err := Render(options); err == nil {
			t.Fatalf("accepted %q", baseURL)
		}
	}
}

func TestRenderIncludesExactCommandAuth(t *testing.T) {
	options := validOptions()
	encoded, err := Render(options)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ModelProviders map[string]struct {
			Auth struct {
				Command           string   `toml:"command"`
				Args              []string `toml:"args"`
				TimeoutMS         int      `toml:"timeout_ms"`
				RefreshIntervalMS int      `toml:"refresh_interval_ms"`
			} `toml:"auth"`
		} `toml:"model_providers"`
	}
	if err := toml.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	auth := decoded.ModelProviders[ProviderID].Auth
	want := []string{"--instance", "default", "--state-dir", options.StateRoot, "--runtime-dir", options.RuntimeRoot, "codex", "token"}
	if auth.Command != options.RouterctlPath || !slices.Equal(auth.Args, want) || auth.TimeoutMS != 5000 || auth.RefreshIntervalMS != 300000 {
		t.Fatalf("auth = %#v", auth)
	}
}

func TestRenderRejectsInvalidOptionsWithoutLeakingValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Options)
	}{
		{"remote URL", func(value *Options) { value.BaseURL = "https://secret.example/v1" }},
		{"relative catalog", func(value *Options) { value.CatalogPath = "models.json" }},
		{"relative command", func(value *Options) { value.RouterctlPath = "routerctl" }},
		{"invalid instance", func(value *Options) { value.Instance = "../../secret" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := validOptions()
			tt.mutate(&options)
			_, err := Render(options)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked input: %v", err)
			}
		})
	}
}
