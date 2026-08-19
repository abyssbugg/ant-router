//go:build !windows

package configstore

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteAtomicUnixPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions test skipped on Windows")
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "perms.txt")
	data := []byte("test data")
	perm := os.FileMode(0600)

	err := writeAtomic(context.Background(), filePath, data, perm)
	if err != nil {
		t.Fatalf("writeAtomic failed: %v", err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	actualPerm := fileInfo.Mode() & os.ModePerm
	if actualPerm != perm {
		t.Errorf("permission mismatch: got %o, want %o", actualPerm, perm)
	}
}

func TestWriteAtomicUnixPermissionsReplace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions test skipped on Windows")
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "perms_replace.txt")
	oldData := []byte("old")
	newData := []byte("new")
	newPerm := os.FileMode(0644)

	// Create with different permissions
	if err := os.WriteFile(filePath, oldData, 0600); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	err := writeAtomic(context.Background(), filePath, newData, newPerm)
	if err != nil {
		t.Fatalf("writeAtomic failed: %v", err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	actualPerm := fileInfo.Mode() & os.ModePerm
	if actualPerm != newPerm {
		t.Errorf("permission mismatch after replace: got %o, want %o", actualPerm, newPerm)
	}
}

func TestWriteAtomicSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on Windows")
	}

	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "target.txt")
	symlinkPath := filepath.Join(tmpDir, "symlink.txt")
	originalData := []byte("original target content")
	newData := []byte("new symlink content")

	// Create target file
	if err := os.WriteFile(targetPath, originalData, 0644); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	// Create symlink pointing to target
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Write atomically to the symlink path
	err := writeAtomic(context.Background(), symlinkPath, newData, 0644)
	if err != nil {
		t.Fatalf("writeAtomic failed: %v", err)
	}

	// Check that symlink is now a regular file
	fileInfo, err := os.Lstat(symlinkPath)
	if err != nil {
		t.Fatalf("failed to lstat symlink path: %v", err)
	}

	if fileInfo.Mode()&os.ModeSymlink != 0 {
		t.Errorf("symlink should have been replaced with a regular file")
	}

	// Verify symlink path contains new data
	content, err := os.ReadFile(symlinkPath)
	if err != nil {
		t.Fatalf("failed to read symlink path: %v", err)
	}

	if string(content) != string(newData) {
		t.Errorf("symlink path content mismatch: got %q, want %q", string(content), string(newData))
	}

	// Verify target file is unchanged
	targetContent, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read target file: %v", err)
	}

	if string(targetContent) != string(originalData) {
		t.Errorf("target file should be unchanged: got %q, want %q", string(targetContent), string(originalData))
	}
}

func TestWriteAtomicSymlinkedParentDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on Windows")
	}

	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	symlinkDir := filepath.Join(tmpDir, "symlink_dir")

	// Create real directory
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatalf("failed to create real directory: %v", err)
	}

	// Create symlink to the directory
	if err := os.Symlink(realDir, symlinkDir); err != nil {
		t.Fatalf("failed to create symlink to directory: %v", err)
	}

	// Try to write through the symlinked parent directory
	targetPath := filepath.Join(symlinkDir, "file.txt")
	err := writeAtomic(context.Background(), targetPath, []byte("test data"), 0644)

	// Should return CodeStoreUnavailable
	if err == nil {
		t.Fatalf("writeAtomic should return error when parent is a symlink")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got code=%q ok=%v", code, ok)
	}

	// Verify target was not created
	_, err = os.Stat(targetPath)
	if !os.IsNotExist(err) {
		t.Errorf("target should not exist when parent is a symlink")
	}
}
