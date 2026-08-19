//go:build windows

package runtimepaths

import (
	"path/filepath"
	"testing"
)

func TestResolveWindowsDefaults(t *testing.T) {
	paths, err := Resolve(Options{LookupEnv: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if !filepath.IsAbs(paths.StateRoot) {
		t.Errorf("StateRoot = %q, want absolute", paths.StateRoot)
	}
	if paths.RuntimeRoot != "" {
		t.Errorf("RuntimeRoot = %q, want empty for named pipes", paths.RuntimeRoot)
	}
}
