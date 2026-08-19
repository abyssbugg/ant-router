//go:build !windows

package runtimepaths

import (
	"path/filepath"
	"testing"
)

func TestResolveUnixDefaults(t *testing.T) {
	paths, err := Resolve(Options{LookupEnv: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if !filepath.IsAbs(paths.StateRoot) || !filepath.IsAbs(paths.RuntimeRoot) {
		t.Errorf("default paths are not absolute: %#v", paths)
	}
	if paths.ConfigRoot != filepath.Join(paths.InstanceDir, "config") {
		t.Errorf("ConfigRoot = %q", paths.ConfigRoot)
	}
}
