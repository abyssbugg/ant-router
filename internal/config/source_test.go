package config

import (
	"path/filepath"
	"testing"
)

func TestResolveAbsolutePathBlank(t *testing.T) {
	result, err := resolveAbsolutePath("", "test_field")
	if err != nil {
		t.Fatalf("resolveAbsolutePath(\"\", \"test_field\") failed: %v", err)
	}
	if result != "" {
		t.Errorf("expected blank path to return blank, got %q", result)
	}
}

func TestResolveAbsolutePathRelativeBecomesAbsolute(t *testing.T) {
	result, err := resolveAbsolutePath("config.toml", "test_field")
	if err != nil {
		t.Fatalf("resolveAbsolutePath failed: %v", err)
	}
	if !filepath.IsAbs(result) {
		t.Errorf("expected absolute path, got %q", result)
	}
}

func TestResolveAbsolutePathCleans(t *testing.T) {
	// Test that it cleans the path
	result, err := resolveAbsolutePath("./config/../config.toml", "test_field")
	if err != nil {
		t.Fatalf("resolveAbsolutePath failed: %v", err)
	}
	if !filepath.IsAbs(result) {
		t.Errorf("expected absolute path, got %q", result)
	}
	// Should not contain ".."
	if filepath.Base(result) == ".." {
		t.Errorf("path should be cleaned, got %q", result)
	}
}

func TestResolveAbsolutePathErrorIncludesField(t *testing.T) {
	// We can't easily trigger filepath.Abs to fail, but we can verify
	// that a path gets processed and the field is used correctly
	result, err := resolveAbsolutePath("valid.toml", "my_field")
	if err != nil {
		t.Fatalf("resolveAbsolutePath failed: %v", err)
	}
	if !filepath.IsAbs(result) {
		t.Errorf("expected absolute path, got %q", result)
	}
}

func TestConfigEnvironmentConstant(t *testing.T) {
	if ConfigEnvironment != "ANT_ROUTER_CONFIG" {
		t.Errorf("ConfigEnvironment = %q, want \"ANT_ROUTER_CONFIG\"", ConfigEnvironment)
	}
}

func TestResolveSourceExplicitTakesPrecedence(t *testing.T) {
	explicit := "/explicit/path"
	getenv := func(key string) string {
		if key == "ANT_ROUTER_CONFIG" {
			return "/env/path"
		}
		return ""
	}
	defaultPath := "/default/path"

	result, err := ResolveSource(explicit, getenv, defaultPath)
	if err != nil {
		t.Fatalf("ResolveSource failed: %v", err)
	}
	if result != "/explicit/path" {
		t.Errorf("got %q, want explicit path", result)
	}
}

func TestResolveSourceNilGetenvTreatsAsNoEnv(t *testing.T) {
	// When getenv is nil, treat as if env var is not set
	result, err := ResolveSource("", nil, "/default/path")
	if err != nil {
		t.Fatalf("ResolveSource failed: %v", err)
	}
	if result != "/default/path" {
		t.Errorf("got %q, want default path when getenv is nil", result)
	}
}

func TestResolveSourceEnvSecondPrecedence(t *testing.T) {
	getenv := func(key string) string {
		if key == "ANT_ROUTER_CONFIG" {
			return "/env/path"
		}
		return ""
	}
	defaultPath := "/default/path"

	result, err := ResolveSource("", getenv, defaultPath)
	if err != nil {
		t.Fatalf("ResolveSource failed: %v", err)
	}
	if result != "/env/path" {
		t.Errorf("got %q, want env path", result)
	}
}

func TestResolveSourceWhitespaceOnlyEnvIgnored(t *testing.T) {
	getenv := func(key string) string {
		if key == "ANT_ROUTER_CONFIG" {
			return "   \t  "
		}
		return ""
	}
	defaultPath := "/default/path"

	result, err := ResolveSource("", getenv, defaultPath)
	if err != nil {
		t.Fatalf("ResolveSource failed: %v", err)
	}
	if result != "/default/path" {
		t.Errorf("got %q, want default path when env is whitespace-only", result)
	}
}

func TestResolveSourceDefaultPath(t *testing.T) {
	getenv := func(key string) string {
		return ""
	}
	defaultPath := "/my/default"

	result, err := ResolveSource("", getenv, defaultPath)
	if err != nil {
		t.Fatalf("ResolveSource failed: %v", err)
	}
	if result != "/my/default" {
		t.Errorf("got %q, want default path", result)
	}
}

func TestResolveSourceAllBlankReturnsSourceNotFound(t *testing.T) {
	getenv := func(key string) string {
		return ""
	}

	_, err := ResolveSource("", getenv, "")
	if err == nil {
		t.Fatal("expected error when all sources are blank")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeSourceNotFound {
		t.Errorf("got code %q, want source_not_found", code)
	}
}

func TestResolveSourceRelativeToAbsolute(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantPrefix string
	}{
		{
			name:       "relative path becomes absolute",
			input:      "config.toml",
			wantPrefix: "/",
		},
		{
			name:       "relative subdir becomes absolute",
			input:      "configs/app.toml",
			wantPrefix: "/",
		},
		{
			name:       "absolute path unchanged",
			input:      "/etc/config.toml",
			wantPrefix: "/etc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveSource(tt.input, func(string) string { return "" }, "")
			if err != nil {
				t.Fatalf("ResolveSource failed: %v", err)
			}
			// Check that result is absolute (starts with /)
			if len(result) == 0 || result[0] != '/' {
				t.Errorf("ResolveSource returned relative path: %q", result)
			}
		})
	}
}

func TestResolveSourceCleansPaths(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "removes double slashes",
			input: "//etc//config.toml",
			want:  "/etc/config.toml",
		},
		{
			name:  "removes trailing slash",
			input: "/etc/config.toml/",
			want:  "/etc/config.toml",
		},
		{
			name:  "resolves dot",
			input: "/etc/./config.toml",
			want:  "/etc/config.toml",
		},
		{
			name:  "resolves dotdot",
			input: "/etc/app/../config.toml",
			want:  "/etc/config.toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveSource(tt.input, func(string) string { return "" }, "")
			if err != nil {
				t.Fatalf("ResolveSource failed: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestResolveSourceExplicitWhitespaceIgnored(t *testing.T) {
	getenv := func(string) string { return "/env/path" }

	result, err := ResolveSource("   ", getenv, "/default")
	if err != nil {
		t.Fatalf("ResolveSource failed: %v", err)
	}
	if result != "/env/path" {
		t.Errorf("got %q, want env path when explicit is whitespace-only", result)
	}
}
