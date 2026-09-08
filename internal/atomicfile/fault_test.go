//go:build !windows

package atomicfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReportsCommittedWhenDirectorySyncFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	injected := errors.New("sync failed")
	result, err := WriteWithHooks(context.Background(), path, []byte("new"), 0o600, &Hooks{
		SyncDirectory: func(string) error { return injected },
	})
	if !result.Committed || !errors.Is(err, injected) {
		t.Fatalf("write = %#v, %v", result, err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "new" {
		t.Fatalf("committed content = %q, %v", got, readErr)
	}
}

func TestWriteRejectsSymlinkedParent(t *testing.T) {
	realDir := t.TempDir()
	root := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	result, err := Write(context.Background(), filepath.Join(link, "state.json"), []byte("secret"), 0o600)
	if err == nil || result.Committed {
		t.Fatalf("write = %#v, %v", result, err)
	}
	if !os.IsNotExist(statError(filepath.Join(realDir, "state.json"))) {
		t.Fatal("write followed symlinked parent")
	}
}
