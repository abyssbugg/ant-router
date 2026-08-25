package runtimepaths

import (
	"path/filepath"
	"testing"
)

func envLookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestResolvePrecedence(t *testing.T) {
	options := Options{
		InstanceOverride: "cli",
		StateOverride:    filepath.Join("cli", "state"),
		RuntimeOverride:  filepath.Join("cli", "runtime"),
		LookupEnv: envLookup(map[string]string{
			EnvironmentInstance:   "environment",
			EnvironmentStateDir:   filepath.Join("environment", "state"),
			EnvironmentRuntimeDir: filepath.Join("environment", "runtime"),
		}),
	}
	paths, err := Resolve(options)
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if paths.Instance.String() != "cli" {
		t.Errorf("Instance = %q, want cli", paths.Instance)
	}
	if paths.StateRoot != absolutePath(t, options.StateOverride) || paths.RuntimeRoot != absolutePath(t, options.RuntimeOverride) {
		t.Errorf("paths = %#v", paths)
	}
	wantInstanceDir := filepath.Join(paths.StateRoot, "instances", "cli")
	if paths.InstanceDir != wantInstanceDir || paths.TokenPath != filepath.Join(wantInstanceDir, "control.token") || paths.ConfigRoot != filepath.Join(wantInstanceDir, "config") {
		t.Errorf("instance paths = %#v", paths)
	}
}

func TestResolveEnvironmentAndWhitespaceFallback(t *testing.T) {
	state := filepath.Join("environment", "state")
	runtime := filepath.Join("environment", "runtime")
	paths, err := Resolve(Options{
		InstanceOverride: "   ",
		StateOverride:    "\t",
		RuntimeOverride:  " ",
		LookupEnv: envLookup(map[string]string{
			EnvironmentInstance:   "dev",
			EnvironmentStateDir:   state,
			EnvironmentRuntimeDir: runtime,
		}),
	})
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if paths.Instance.String() != "dev" || paths.StateRoot != absolutePath(t, state) || paths.RuntimeRoot != absolutePath(t, runtime) {
		t.Errorf("paths = %#v", paths)
	}
}

func TestResolvePreservesLegitimatePathSpaces(t *testing.T) {
	state := filepath.Join("state root", "with spaces")
	runtime := filepath.Join("runtime root", "with spaces")
	paths, err := Resolve(Options{InstanceOverride: "default", StateOverride: state, RuntimeOverride: runtime})
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if paths.StateRoot != absolutePath(t, state) || paths.RuntimeRoot != absolutePath(t, runtime) {
		t.Errorf("paths = %#v", paths)
	}
}

func TestResolveRejectsInvalidInstance(t *testing.T) {
	_, err := Resolve(Options{InstanceOverride: "../escape", StateOverride: t.TempDir(), RuntimeOverride: t.TempDir()})
	if err == nil {
		t.Fatal("Resolve() accepted invalid instance")
	}
}

func TestResolveUsesDefaultInstance(t *testing.T) {
	paths, err := Resolve(Options{StateOverride: t.TempDir(), RuntimeOverride: t.TempDir(), LookupEnv: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if paths.Instance.String() != "default" {
		t.Errorf("Instance = %q, want default", paths.Instance)
	}
}

func absolutePath(t *testing.T, value string) string {
	t.Helper()
	absolute, err := filepath.Abs(value)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) failed: %v", value, err)
	}
	return filepath.Clean(absolute)
}
