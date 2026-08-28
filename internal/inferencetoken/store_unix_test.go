//go:build !windows

package inferencetoken

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/abyssbugg/ant-router/internal/controltransport"
)

func TestStoreUnixPermissions(t *testing.T) {
	store := newTestStore(t)
	store.random = bytes.NewReader(bytes.Repeat([]byte{0x41}, TokenSize))
	if _, err := store.LoadOrCreate(controltransport.DefaultInstance); err != nil {
		t.Fatalf("LoadOrCreate() failed: %v", err)
	}
	paths, _ := store.resolveInstance(controltransport.DefaultInstance)
	for _, path := range []string{store.stateRoot, filepath.Dir(paths.StateDir), paths.StateDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat(%q) failed: %v", path, err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("directory %q mode = %04o, want 0700", path, info.Mode().Perm())
		}
	}
	info, err := os.Stat(paths.TokenPath)
	if err != nil {
		t.Fatalf("os.Stat(token) failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestStoreRejectsSymlinks(t *testing.T) {
	store := newTestStore(t)
	paths, _ := store.resolveInstance(controltransport.DefaultInstance)
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(target) failed: %v", err)
	}
	if err := os.Symlink(target, paths.TokenPath); err != nil {
		t.Fatalf("os.Symlink() failed: %v", err)
	}
	if _, err := store.Load(controltransport.DefaultInstance); CodeOfValue(err) != CodeTokenCorrupt {
		t.Fatalf("token symlink code = %q, want %q", CodeOfValue(err), CodeTokenCorrupt)
	}
}
