//go:build !windows
// +build !windows

package configstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

// TestNewFileStoreRootSymlinkUnix tests rejection of symlink root on Unix.
func TestNewFileStoreRootSymlinkUnix(t *testing.T) {
	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	linkPath := filepath.Join(tmpDir, "link")

	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatalf("failed to create real directory: %v", err)
	}
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	_, err := NewFileStore(linkPath)
	if err == nil {
		t.Fatal("NewFileStore should reject symlink root")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestNewFileStoreRevisionsDirSymlinkUnix tests rejection of revisions dir symlink on Unix.
func TestNewFileStoreRevisionsDirSymlinkUnix(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "store")
	realDir := filepath.Join(tmpDir, "real_revisions")
	linkPath := filepath.Join(storeDir, "revisions")

	if err := os.Mkdir(storeDir, 0700); err != nil {
		t.Fatalf("failed to create store directory: %v", err)
	}
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatalf("failed to create real revisions directory: %v", err)
	}
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	_, err := NewFileStore(storeDir)
	if err == nil {
		t.Fatal("NewFileStore should reject revisions symlink")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestNewFileStorePermissionsUnix tests that directories are created with 0700 permissions on Unix.
func TestNewFileStorePermissionsUnix(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "store")

	fs, err := NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	rootStat, _ := os.Stat(fs.root)
	revisionsStat, _ := os.Stat(fs.revisionsDir)

	// Check permissions (0700 = drwx------)
	if rootStat.Mode().Perm() != 0700 {
		t.Errorf("root permissions %o, want 0700", rootStat.Mode().Perm())
	}
	if revisionsStat.Mode().Perm() != 0700 {
		t.Errorf("revisionsDir permissions %o, want 0700", revisionsStat.Mode().Perm())
	}
}

// TestReadRevisionSymlinkUnix tests rejection of symlink on Unix.
func TestReadRevisionSymlinkUnix(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	data := []byte("test data")
	id := revision.For(data)

	// Create a symlink instead of a regular file
	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")
	targetPath := filepath.Join(tmpDir, "target")
	if err := os.WriteFile(targetPath, data, 0600); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}
	if err := os.Symlink(targetPath, revPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	_, err = fs.ReadRevision(ctx, id)
	if err == nil {
		t.Fatal("ReadRevision should reject symlink")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeRevisionCorrupt {
		t.Errorf("expected CodeRevisionCorrupt, got %q", code)
	}
}

// TestReadManifestSymlinkUnix tests rejection of symlink on Unix.
func TestReadManifestSymlinkUnix(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	// Create a symlink instead of a regular file
	targetPath := filepath.Join(tmpDir, "target")
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   time.Now(),
	}
	encoded, _ := EncodeManifest(manifest)
	if err := os.WriteFile(targetPath, encoded, 0600); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}
	if err := os.Symlink(targetPath, fs.manifestPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	_, err = fs.ReadManifest(ctx)
	if err == nil {
		t.Fatal("ReadManifest should reject symlink")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeManifestCorrupt {
		t.Errorf("expected CodeManifestCorrupt, got %q", code)
	}
}

// TestCleanupTempsSymlinkUnix tests removal of temp symlinks on Unix.
func TestCleanupTempsSymlinkUnix(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	targetPath := filepath.Join(tmpDir, "target")
	if err := os.WriteFile(targetPath, []byte("test"), 0600); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	tmpSymlink := filepath.Join(fs.root, ".manifest.json.tmp-12345")
	if err := os.Symlink(targetPath, tmpSymlink); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	if err := fs.CleanupTemps(ctx); err != nil {
		t.Fatalf("CleanupTemps failed: %v", err)
	}

	// Verify symlink was removed
	if _, err := os.Lstat(tmpSymlink); !os.IsNotExist(err) {
		t.Errorf("temp symlink not removed: %v", err)
	}
}
