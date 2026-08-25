package runtimepaths

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzResolveInstanceIsolation(f *testing.F) {
	f.Add("default")
	f.Add("test-1")
	f.Add("../escape")
	f.Fuzz(func(t *testing.T, instance string) {
		root := filepath.Join(os.TempDir(), "ant-router-runtimepaths-fuzz")
		paths, err := Resolve(Options{InstanceOverride: instance, StateOverride: root, RuntimeOverride: root, LookupEnv: func(string) string { return "" }})
		if err != nil {
			return
		}
		relative, err := filepath.Rel(paths.StateRoot, paths.InstanceDir)
		if err != nil {
			t.Fatalf("filepath.Rel() failed: %v", err)
		}
		if !filepath.IsLocal(filepath.Clean(relative)) {
			t.Fatalf("instance escaped state root: %q", relative)
		}
	})
}
