package configstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

func regressionManifest() Manifest {
	return Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("current")),
		ActivatedAt:   time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
	}
}

func TestCleanupTempsRemovesOnlyOwnedPatterns(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}

	id := revision.For([]byte("revision"))
	ownedRoot := filepath.Join(store.root, ".manifest.json.tmp-owned")
	unrelatedRoot := filepath.Join(store.root, ".unrelated.tmp-keep")
	ownedRevision := filepath.Join(store.revisionsDir, "."+id.String()+".toml.tmp-owned")
	unrelatedRevision := filepath.Join(store.revisionsDir, ".not-a-revision.toml.tmp-keep")
	for _, path := range []string{ownedRoot, unrelatedRoot, ownedRevision, unrelatedRevision} {
		if err := os.WriteFile(path, []byte("temporary"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q) failed: %v", path, err)
		}
	}

	if err := store.CleanupTemps(context.Background()); err != nil {
		t.Fatalf("CleanupTemps() failed: %v", err)
	}

	for _, path := range []string{ownedRoot, ownedRevision} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("owned temporary file %q still exists: %v", path, err)
		}
	}
	for _, path := range []string{unrelatedRoot, unrelatedRevision} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("unrelated file %q was removed: %v", path, err)
		}
	}
}

func TestWriteManifestMissingCurrentIsManifestCorrupt(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manifest := regressionManifest()

	err = store.WriteManifest(context.Background(), manifest)
	if code, ok := CodeOf(err); !ok || code != CodeManifestCorrupt {
		t.Fatalf("WriteManifest() code = %q, present = %v, want %q; error: %v", code, ok, CodeManifestCorrupt, err)
	}
}
