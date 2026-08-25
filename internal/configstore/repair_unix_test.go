//go:build !windows

package configstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/abyssbugg/ant-router/internal/revision"
)

func TestRepairRevisionRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	data := []byte("canonical")
	id := revision.For(data)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(target) failed: %v", err)
	}
	path := filepath.Join(root, "revisions", id.String()+".toml")
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("os.Symlink() failed: %v", err)
	}
	if err := store.RepairRevision(context.Background(), id, data); CodeValue(err) != CodeRevisionCorrupt {
		t.Fatalf("symlink repair code = %q, want %q", CodeValue(err), CodeRevisionCorrupt)
	}
}
