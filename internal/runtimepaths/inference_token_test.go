package runtimepaths

import (
	"path/filepath"
	"testing"
)

func TestResolveSeparatesInferenceAndControlTokens(t *testing.T) {
	paths, err := Resolve(Options{
		InstanceOverride: "dev",
		StateOverride:    t.TempDir(),
		RuntimeOverride:  t.TempDir(),
		LookupEnv:        func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	want := filepath.Join(paths.InstanceDir, "inference.token")
	if paths.InferenceTokenPath != want {
		t.Fatalf("InferenceTokenPath = %q, want %q", paths.InferenceTokenPath, want)
	}
	if paths.InferenceTokenPath == paths.TokenPath {
		t.Fatal("inference and control token paths must differ")
	}
}
