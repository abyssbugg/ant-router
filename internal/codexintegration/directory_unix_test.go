//go:build !windows

package codexintegration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDirectoryRequiresPrivateRealDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateDirectory(root); err == nil {
		t.Fatal("public Codex home accepted")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateDirectory(root); err != nil {
		t.Fatalf("private Codex home rejected: %v", err)
	}
	link := filepath.Join(filepath.Dir(root), "codex-home-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := validateDirectory(link); err == nil {
		t.Fatal("symlinked Codex home accepted")
	}
}

func TestObserveFileRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "profile")
	if err := os.WriteFile(target, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := observeFile(context.Background(), link, 1024); err == nil {
		t.Fatal("symlinked file accepted")
	}
}
