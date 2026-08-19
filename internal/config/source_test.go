package config

import (
	"path/filepath"
	"testing"
)

func expectedAbsolutePath(t *testing.T, value string) string {
	t.Helper()
	absolute, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		t.Fatalf("filepath.Abs(%q) failed: %v", value, err)
	}
	return filepath.Clean(absolute)
}

func TestResolveAbsolutePathBlank(t *testing.T) {
	result, err := resolveAbsolutePath("", "test_field")
	if err != nil {
		t.Fatalf("resolveAbsolutePath() failed: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want blank", result)
	}
}

func TestResolveAbsolutePathRelativeAndClean(t *testing.T) {
	input := filepath.Join("config", "..", "config.toml")
	result, err := resolveAbsolutePath(input, "test_field")
	if err != nil {
		t.Fatalf("resolveAbsolutePath() failed: %v", err)
	}
	if !filepath.IsAbs(result) {
		t.Errorf("result = %q, want absolute", result)
	}
	if result != expectedAbsolutePath(t, input) {
		t.Errorf("result = %q, want %q", result, expectedAbsolutePath(t, input))
	}
}

func TestConfigEnvironmentConstant(t *testing.T) {
	if ConfigEnvironment != "ANT_ROUTER_CONFIG" {
		t.Errorf("ConfigEnvironment = %q, want ANT_ROUTER_CONFIG", ConfigEnvironment)
	}
}

func TestResolveSourcePrecedence(t *testing.T) {
	explicit := filepath.Join("explicit", "config.toml")
	environment := filepath.Join("environment", "config.toml")
	fallback := filepath.Join("default", "config.toml")
	getenv := func(key string) string {
		if key == ConfigEnvironment {
			return environment
		}
		return ""
	}

	tests := []struct {
		name        string
		explicit    string
		getenv      func(string) string
		defaultPath string
		want        string
	}{
		{name: "explicit", explicit: explicit, getenv: getenv, defaultPath: fallback, want: explicit},
		{name: "environment", getenv: getenv, defaultPath: fallback, want: environment},
		{name: "nil environment", getenv: nil, defaultPath: fallback, want: fallback},
		{name: "whitespace environment", getenv: func(string) string { return "  \t " }, defaultPath: fallback, want: fallback},
		{name: "whitespace explicit", explicit: "   ", getenv: getenv, defaultPath: fallback, want: environment},
		{name: "default", getenv: func(string) string { return "" }, defaultPath: fallback, want: fallback},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := ResolveSource(test.explicit, test.getenv, test.defaultPath)
			if err != nil {
				t.Fatalf("ResolveSource() failed: %v", err)
			}
			want := expectedAbsolutePath(t, test.want)
			if result != want {
				t.Errorf("result = %q, want %q", result, want)
			}
		})
	}
}

func TestResolveSourceAllBlank(t *testing.T) {
	_, err := ResolveSource("", func(string) string { return "" }, "")
	if code, ok := CodeOf(err); !ok || code != CodeSourceNotFound {
		t.Fatalf("code = %q, present = %v, want %q", code, ok, CodeSourceNotFound)
	}
}

func TestResolveSourceTrimsSelectedValue(t *testing.T) {
	input := filepath.Join("trimmed", "config.toml")
	result, err := ResolveSource("  "+input+"  ", nil, "")
	if err != nil {
		t.Fatalf("ResolveSource() failed: %v", err)
	}
	if result != expectedAbsolutePath(t, input) {
		t.Errorf("result = %q, want %q", result, expectedAbsolutePath(t, input))
	}
}
