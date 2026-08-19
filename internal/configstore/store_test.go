package configstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

// TestNewFileStoreAbsolutePathConversion tests that paths are converted to absolute.
func TestNewFileStoreAbsolutePathConversion(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with absolute path
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}
	if !filepath.IsAbs(fs.root) {
		t.Errorf("root not absolute: %v", fs.root)
	}
	if !filepath.IsAbs(fs.revisionsDir) {
		t.Errorf("revisionsDir not absolute: %v", fs.revisionsDir)
	}
	if !filepath.IsAbs(fs.manifestPath) {
		t.Errorf("manifestPath not absolute: %v", fs.manifestPath)
	}
}

// TestNewFileStoreCreatesDirectories tests that root and revisions directories are created.
func TestNewFileStoreCreatesDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "store")

	fs, err := NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	// Check that directories exist
	if stat, err := os.Stat(fs.root); err != nil || !stat.IsDir() {
		t.Errorf("root directory not created: %v", err)
	}
	if stat, err := os.Stat(fs.revisionsDir); err != nil || !stat.IsDir() {
		t.Errorf("revisionsDir not created: %v", err)
	}
}

// TestNewFileStoreBlankPath tests that blank path is rejected.
func TestNewFileStoreBlankPath(t *testing.T) {
	_, err := NewFileStore("")
	if err == nil {
		t.Fatal("NewFileStore should reject blank path")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestNewFileStoreNonDirectory tests that non-directory path is rejected.
func TestNewFileStoreNonDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file")

	// Create a regular file
	if err := os.WriteFile(filePath, []byte("test"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := NewFileStore(filePath)
	if err == nil {
		t.Fatal("NewFileStore should reject non-directory path")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestNewFileStoreRootSymlink tests rejection of symlink root (Unix only).
func TestNewFileStoreRootSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	linkPath := filepath.Join(tmpDir, "link")

	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatalf("failed to create real directory: %v", err)
	}
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Skip("symlink creation not supported")
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

// TestNewFileStoreRevisionsDirSymlink tests rejection of revisions dir symlink (Unix only).
func TestNewFileStoreRevisionsDirSymlink(t *testing.T) {
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
		t.Skip("symlink creation not supported")
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

// TestNewFileStorePermissions tests that directories are created with owner-only permissions.
func TestNewFileStorePermissions(t *testing.T) {
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

// TestPutRevisionContextCancellation tests context cancellation.
func TestPutRevisionContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	data := []byte("test data")
	id := revision.For(data)

	err = fs.PutRevision(ctx, id, data)
	if err == nil {
		t.Fatal("PutRevision should fail with cancelled context")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeCancelled {
		t.Errorf("expected CodeCancelled, got %q", code)
	}
}

// TestPutRevisionInvalidID tests rejection of invalid revision ID.
func TestPutRevisionInvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	data := []byte("test data")
	invalidID := revision.ID("invalid")

	err = fs.PutRevision(ctx, invalidID, data)
	if err == nil {
		t.Fatal("PutRevision should fail with invalid ID")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeInvalidRevision {
		t.Errorf("expected CodeInvalidRevision, got %q", code)
	}
}

// TestPutRevisionHashMismatch tests rejection when hash doesn't match data.
func TestPutRevisionHashMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	data := []byte("test data")
	wrongID := revision.For([]byte("different data"))

	err = fs.PutRevision(ctx, wrongID, data)
	if err == nil {
		t.Fatal("PutRevision should fail with hash mismatch")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeInvalidRevision {
		t.Errorf("expected CodeInvalidRevision, got %q", code)
	}
}

// TestPutRevisionOversizeData tests rejection of oversized data.
func TestPutRevisionOversizeData(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	oversizeData := make([]byte, MaxRevisionBytes+1)
	id := revision.For(oversizeData)

	err = fs.PutRevision(ctx, id, oversizeData)
	if err == nil {
		t.Fatal("PutRevision should fail with oversize data")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestPutRevisionCreateAndRead tests creating and reading a revision.
func TestPutRevisionCreateAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	data := []byte("test data")
	id := revision.For(data)

	if err := fs.PutRevision(ctx, id, data); err != nil {
		t.Fatalf("PutRevision failed: %v", err)
	}

	// Verify file was created
	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")
	if _, err := os.Stat(revPath); err != nil {
		t.Errorf("revision file not created: %v", err)
	}

	// Read it back
	readData, err := fs.ReadRevision(ctx, id)
	if err != nil {
		t.Fatalf("ReadRevision failed: %v", err)
	}
	if string(readData) != string(data) {
		t.Errorf("ReadRevision returned wrong data: got %q, want %q", readData, data)
	}
}

// TestPutRevisionIdempotent tests that putting the same revision twice is safe.
func TestPutRevisionIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	data := []byte("test data")
	id := revision.For(data)

	if err := fs.PutRevision(ctx, id, data); err != nil {
		t.Fatalf("first PutRevision failed: %v", err)
	}

	if err := fs.PutRevision(ctx, id, data); err != nil {
		t.Fatalf("second PutRevision failed: %v", err)
	}

	// Verify data is still correct
	readData, err := fs.ReadRevision(ctx, id)
	if err != nil {
		t.Fatalf("ReadRevision failed: %v", err)
	}
	if string(readData) != string(data) {
		t.Errorf("ReadRevision returned wrong data after idempotent put")
	}
}

// TestReadRevisionInvalidID tests rejection of invalid revision ID.
func TestReadRevisionInvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	invalidID := revision.ID("invalid")

	_, err = fs.ReadRevision(ctx, invalidID)
	if err == nil {
		t.Fatal("ReadRevision should fail with invalid ID")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeInvalidRevision {
		t.Errorf("expected CodeInvalidRevision, got %q", code)
	}
}

// TestReadRevisionMissing tests error when revision file is missing.
func TestReadRevisionMissing(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	id := revision.For([]byte("nonexistent"))

	_, err = fs.ReadRevision(ctx, id)
	if err == nil {
		t.Fatal("ReadRevision should fail for missing file")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestReadRevisionSymlink tests rejection of symlink.
func TestReadRevisionSymlink(t *testing.T) {
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
		t.Skip("symlink creation not supported")
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

// TestReadRevisionDirectory tests rejection of directory.
func TestReadRevisionDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	id := revision.For([]byte("test"))

	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")
	if err := os.Mkdir(revPath, 0700); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	_, err = fs.ReadRevision(ctx, id)
	if err == nil {
		t.Fatal("ReadRevision should reject directory")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeRevisionCorrupt {
		t.Errorf("expected CodeRevisionCorrupt, got %q", code)
	}
}

// TestReadRevisionOversizeFile tests rejection of oversized file.
func TestReadRevisionOversizeFile(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	oversizeData := make([]byte, MaxRevisionBytes+1)
	id := revision.For(oversizeData)

	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")
	if err := os.WriteFile(revPath, oversizeData, 0600); err != nil {
		t.Fatalf("failed to write oversized file: %v", err)
	}

	_, err = fs.ReadRevision(ctx, id)
	if err == nil {
		t.Fatal("ReadRevision should fail for oversized file")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestReadRevisionHashMismatch tests rejection when file hash doesn't match ID.
func TestReadRevisionHashMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	expectedData := []byte("expected data")
	id := revision.For(expectedData)

	actualData := []byte("different data")
	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")
	if err := os.WriteFile(revPath, actualData, 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err = fs.ReadRevision(ctx, id)
	if err == nil {
		t.Fatal("ReadRevision should fail on hash mismatch")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeRevisionCorrupt {
		t.Errorf("expected CodeRevisionCorrupt, got %q", code)
	}
}

// TestReadRevisionContextCancellation tests context cancellation.
func TestReadRevisionContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	data := []byte("test data")
	id := revision.For(data)

	_, err = fs.ReadRevision(ctx, id)
	if err == nil {
		t.Fatal("ReadRevision should fail with cancelled context")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeCancelled {
		t.Errorf("expected CodeCancelled, got %q", code)
	}
}

// TestConcurrentSameRevisionPuts tests concurrent puts of the same revision.
func TestConcurrentSameRevisionPuts(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	data := []byte("test data")
	id := revision.For(data)

	// Launch multiple concurrent puts
	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			done <- fs.PutRevision(ctx, id, data)
		}()
	}

	// Collect results
	for i := 0; i < 5; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent PutRevision failed: %v", err)
		}
	}

	// Verify data is correct
	readData, err := fs.ReadRevision(ctx, id)
	if err != nil {
		t.Fatalf("ReadRevision failed: %v", err)
	}
	if string(readData) != string(data) {
		t.Errorf("ReadRevision returned wrong data after concurrent puts")
	}
}

// TestReadManifestMissing tests error when manifest is missing.
func TestReadManifestMissing(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	_, err = fs.ReadManifest(ctx)
	if err == nil {
		t.Fatal("ReadManifest should fail when manifest is missing")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeManifestNotFound {
		t.Errorf("expected CodeManifestNotFound, got %q", code)
	}
}

// TestReadManifestSymlink tests rejection of symlink.
func TestReadManifestSymlink(t *testing.T) {
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
		t.Skip("symlink creation not supported")
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

// TestReadManifestDirectory tests rejection of directory.
func TestReadManifestDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	if err := os.Mkdir(fs.manifestPath, 0700); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	_, err = fs.ReadManifest(ctx)
	if err == nil {
		t.Fatal("ReadManifest should reject directory")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeManifestCorrupt {
		t.Errorf("expected CodeManifestCorrupt, got %q", code)
	}
}

// TestReadManifestOversizeFile tests rejection of oversized file.
func TestReadManifestOversizeFile(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	oversizeData := make([]byte, MaxManifestBytes+1)
	if err := os.WriteFile(fs.manifestPath, oversizeData, 0600); err != nil {
		t.Fatalf("failed to write oversized file: %v", err)
	}

	_, err = fs.ReadManifest(ctx)
	if err == nil {
		t.Fatal("ReadManifest should fail for oversized file")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestReadManifestCorrupt tests rejection of corrupt manifest.
func TestReadManifestCorrupt(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	if err := os.WriteFile(fs.manifestPath, []byte("invalid json"), 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err = fs.ReadManifest(ctx)
	if err == nil {
		t.Fatal("ReadManifest should fail for corrupt manifest")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeManifestCorrupt {
		t.Errorf("expected CodeManifestCorrupt, got %q", code)
	}
}

// TestWriteManifestContextCancellation tests context cancellation.
func TestWriteManifestContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   time.Now(),
	}

	err = fs.WriteManifest(ctx, manifest)
	if err == nil {
		t.Fatal("WriteManifest should fail with cancelled context")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeCancelled {
		t.Errorf("expected CodeCancelled, got %q", code)
	}
}

// TestWriteManifestCreateAndRead tests writing and reading a manifest.
func TestWriteManifestCreateAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	// First, put the revision it references
	if err := fs.PutRevision(ctx, manifest.Current, []byte("test")); err != nil {
		t.Fatalf("PutRevision failed: %v", err)
	}

	if err := fs.WriteManifest(ctx, manifest); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}

	readManifest, err := fs.ReadManifest(ctx)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}

	if readManifest.Generation != manifest.Generation {
		t.Errorf("Generation mismatch: got %d, want %d", readManifest.Generation, manifest.Generation)
	}
	if readManifest.Current != manifest.Current {
		t.Errorf("Current mismatch: got %s, want %s", readManifest.Current, manifest.Current)
	}
}

// TestWriteManifestWithPrevious tests writing a manifest with previous revision.
func TestWriteManifestWithPrevious(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	prevID := revision.For([]byte("previous"))
	currentID := revision.For([]byte("current"))

	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    2,
		Current:       currentID,
		Previous:      prevID,
		ActivatedAt:   now,
	}

	// Put both revisions
	if err := fs.PutRevision(ctx, prevID, []byte("previous")); err != nil {
		t.Fatalf("PutRevision for previous failed: %v", err)
	}
	if err := fs.PutRevision(ctx, currentID, []byte("current")); err != nil {
		t.Fatalf("PutRevision for current failed: %v", err)
	}

	if err := fs.WriteManifest(ctx, manifest); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}

	readManifest, err := fs.ReadManifest(ctx)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}

	if readManifest.Previous != manifest.Previous {
		t.Errorf("Previous mismatch: got %s, want %s", readManifest.Previous, manifest.Previous)
	}
}

// TestWriteManifestMissingCurrentRevision tests rejection when current revision doesn't exist.
func TestWriteManifestMissingCurrentRevision(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("nonexistent")),
		Previous:      "",
		ActivatedAt:   now,
	}

	err = fs.WriteManifest(ctx, manifest)
	if err == nil {
		t.Fatal("WriteManifest should fail when current revision doesn't exist")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeManifestCorrupt {
		t.Errorf("expected CodeManifestCorrupt, got %q", code)
	}
}

// TestWriteManifestMissingPreviousRevision tests rejection when previous revision doesn't exist.
func TestWriteManifestMissingPreviousRevision(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC()
	currentID := revision.For([]byte("current"))

	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    2,
		Current:       currentID,
		Previous:      revision.For([]byte("nonexistent")),
		ActivatedAt:   now,
	}

	// Put only current revision
	if err := fs.PutRevision(ctx, currentID, []byte("current")); err != nil {
		t.Fatalf("PutRevision for current failed: %v", err)
	}

	err = fs.WriteManifest(ctx, manifest)
	if err == nil {
		t.Fatal("WriteManifest should fail when previous revision doesn't exist")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeManifestCorrupt {
		t.Errorf("expected CodeManifestCorrupt, got %q", code)
	}
}

// TestWriteManifestInvalidManifest tests rejection of invalid manifest.
func TestWriteManifestInvalidManifest(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	// Invalid manifest: zero generation
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    0,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   time.Now(),
	}

	err = fs.WriteManifest(ctx, manifest)
	if err == nil {
		t.Fatal("WriteManifest should fail with invalid manifest")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeManifestCorrupt {
		t.Errorf("expected CodeManifestCorrupt, got %q", code)
	}
}

// TestWriteManifestWriteFileFailurePropagation tests that writeFile failures are propagated.
func TestWriteManifestWriteFileFailurePropagation(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	currentID := revision.For([]byte("current"))
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       currentID,
		Previous:      "",
		ActivatedAt:   time.Now().UTC(),
	}

	// Put the revision
	if err := fs.PutRevision(ctx, currentID, []byte("current")); err != nil {
		t.Fatalf("PutRevision failed: %v", err)
	}

	// Replace writeFile with a failing one
	originalWriteFile := fs.writeFile
	fs.writeFile = func(ctx context.Context, path string, data []byte, perm os.FileMode) error {
		return &Error{Code: CodeStoreUnavailable, Err: fmt.Errorf("injected failure")}
	}
	defer func() { fs.writeFile = originalWriteFile }()

	err = fs.WriteManifest(ctx, manifest)
	if err == nil {
		t.Fatal("WriteManifest should propagate writeFile failure")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStoreUnavailable {
		t.Errorf("expected CodeStoreUnavailable, got %q", code)
	}
}

// TestWriteManifestDurabilityUncertainPropagation tests that CodeDurabilityUncertain is propagated.
func TestWriteManifestDurabilityUncertainPropagation(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	currentID := revision.For([]byte("current"))
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       currentID,
		Previous:      "",
		ActivatedAt:   time.Now().UTC(),
	}

	// Put the revision
	if err := fs.PutRevision(ctx, currentID, []byte("current")); err != nil {
		t.Fatalf("PutRevision failed: %v", err)
	}

	// Replace writeFile with one that returns CodeDurabilityUncertain
	originalWriteFile := fs.writeFile
	fs.writeFile = func(ctx context.Context, path string, data []byte, perm os.FileMode) error {
		return &Error{Code: CodeDurabilityUncertain, Err: fmt.Errorf("sync failed")}
	}
	defer func() { fs.writeFile = originalWriteFile }()

	err = fs.WriteManifest(ctx, manifest)
	if err == nil {
		t.Fatal("WriteManifest should propagate CodeDurabilityUncertain")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeDurabilityUncertain {
		t.Errorf("expected CodeDurabilityUncertain, got %q", code)
	}
}

// TestCleanupTempsRecognizedPatterns tests cleanup of recognized temp patterns.
func TestCleanupTempsRecognizedPatterns(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	// Create temp files matching writeAtomic's exact patterns.
	id := revision.For([]byte("temporary revision"))
	tmpFile1 := filepath.Join(fs.root, ".manifest.json.tmp-12345")
	tmpFile2 := filepath.Join(fs.revisionsDir, "."+id.String()+".toml.tmp-67890")

	if err := os.WriteFile(tmpFile1, []byte("temp"), 0600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := os.WriteFile(tmpFile2, []byte("temp"), 0600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	if err := fs.CleanupTemps(ctx); err != nil {
		t.Fatalf("CleanupTemps failed: %v", err)
	}

	// Verify files were removed
	if _, err := os.Stat(tmpFile1); !os.IsNotExist(err) {
		t.Errorf("temp file 1 not removed: %v", err)
	}
	if _, err := os.Stat(tmpFile2); !os.IsNotExist(err) {
		t.Errorf("temp file 2 not removed: %v", err)
	}
}

// TestCleanupTempsSymlinkPattern tests removal of temp symlinks.
func TestCleanupTempsSymlinkPattern(t *testing.T) {
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
		t.Skip("symlink creation not supported")
	}

	if err := fs.CleanupTemps(ctx); err != nil {
		t.Fatalf("CleanupTemps failed: %v", err)
	}

	// Verify symlink was removed
	if _, err := os.Lstat(tmpSymlink); !os.IsNotExist(err) {
		t.Errorf("temp symlink not removed: %v", err)
	}
}

// TestCleanupTempsIgnoresUnrelatedHiddenFiles tests that unrelated hidden files are not removed.
func TestCleanupTempsIgnoresUnrelatedHiddenFiles(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	// Create an unrelated hidden file
	hiddenFile := filepath.Join(fs.root, ".gitkeep")
	if err := os.WriteFile(hiddenFile, []byte("keep"), 0600); err != nil {
		t.Fatalf("failed to create hidden file: %v", err)
	}

	if err := fs.CleanupTemps(ctx); err != nil {
		t.Fatalf("CleanupTemps failed: %v", err)
	}

	// Verify file was not removed
	if _, err := os.Stat(hiddenFile); err != nil {
		t.Errorf("unrelated hidden file was removed: %v", err)
	}
}

// TestCleanupTempsIgnoresDirectories tests that directories are not removed.
func TestCleanupTempsIgnoresDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()

	// Create a directory with temp-like name
	tmpDir2 := filepath.Join(fs.root, ".manifest.tmp-12345")
	if err := os.Mkdir(tmpDir2, 0700); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	if err := fs.CleanupTemps(ctx); err != nil {
		t.Fatalf("CleanupTemps failed: %v", err)
	}

	// Verify directory was not removed
	if _, err := os.Stat(tmpDir2); err != nil {
		t.Errorf("directory was removed: %v", err)
	}
}

// TestCleanupTempsCancellation tests cancellation during cleanup.
func TestCleanupTempsCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	// Create multiple temp files
	for i := 0; i < 10; i++ {
		tmpFile := filepath.Join(fs.root, fmt.Sprintf(".file.tmp-%d", i))
		if err := os.WriteFile(tmpFile, []byte("temp"), 0600); err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = fs.CleanupTemps(ctx)
	if err == nil {
		t.Fatal("CleanupTemps should fail with cancelled context")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeCancelled {
		t.Errorf("expected CodeCancelled, got %q", code)
	}
}

// TestReadRevisionDefensiveBytes tests that returned bytes are defensive copy.
func TestReadRevisionDefensiveBytes(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	data := []byte("test data")
	id := revision.For(data)

	if err := fs.PutRevision(ctx, id, data); err != nil {
		t.Fatalf("PutRevision failed: %v", err)
	}

	readData, err := fs.ReadRevision(ctx, id)
	if err != nil {
		t.Fatalf("ReadRevision failed: %v", err)
	}

	// Modify returned bytes
	if len(readData) > 0 {
		readData[0] = 'X'
	}

	// Read again and verify original is unchanged
	readData2, err := fs.ReadRevision(ctx, id)
	if err != nil {
		t.Fatalf("second ReadRevision failed: %v", err)
	}

	if string(readData2) != string(data) {
		t.Errorf("original data was modified: got %q, want %q", readData2, data)
	}
}

// TestPutRevisionPermissions tests that revision files have 0600 permissions.
func TestPutRevisionPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	data := []byte("test data")
	id := revision.For(data)

	if err := fs.PutRevision(ctx, id, data); err != nil {
		t.Fatalf("PutRevision failed: %v", err)
	}

	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")
	stat, _ := os.Stat(revPath)
	if stat.Mode().Perm() != 0600 {
		t.Errorf("revision file permissions %o, want 0600", stat.Mode().Perm())
	}
}

// TestWriteManifestPermissions tests that manifest file has 0600 permissions.
func TestWriteManifestPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	currentID := revision.For([]byte("current"))
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       currentID,
		Previous:      "",
		ActivatedAt:   time.Now().UTC(),
	}

	// Put the revision
	if err := fs.PutRevision(ctx, currentID, []byte("current")); err != nil {
		t.Fatalf("PutRevision failed: %v", err)
	}

	if err := fs.WriteManifest(ctx, manifest); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}

	stat, _ := os.Stat(fs.manifestPath)
	if stat.Mode().Perm() != 0600 {
		t.Errorf("manifest file permissions %o, want 0600", stat.Mode().Perm())
	}
}

// TestReadRevisionReadError tests handling of read errors.
func TestReadRevisionReadError(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	id := revision.For([]byte("test"))
	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")

	// Create a readable file that becomes unreadable
	if err := os.WriteFile(revPath, []byte("test"), 0600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Make it unreadable
	if err := os.Chmod(revPath, 0000); err != nil {
		t.Fatalf("failed to change permissions: %v", err)
	}
	defer os.Chmod(revPath, 0600) // restore for cleanup

	_, err = fs.ReadRevision(ctx, id)
	if err == nil {
		t.Fatal("ReadRevision should fail when file is unreadable")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeRevisionCorrupt {
		t.Errorf("expected CodeRevisionCorrupt for insecure permissions, got %q", code)
	}
}
