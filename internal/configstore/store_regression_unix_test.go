//go:build !windows

package configstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/abyssbugg/ant-router/internal/revision"
)

func TestNewFileStoreTightensExistingDirectoryPermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("os.Mkdir(root) failed: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "revisions"), 0o755); err != nil {
		t.Fatalf("os.Mkdir(revisions) failed: %v", err)
	}

	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	for _, path := range []string{store.root, store.revisionsDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat(%q) failed: %v", path, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("permissions for %q = %04o, want 0700", path, got)
		}
	}
}

func TestReadRevisionRejectsInsecurePermissions(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	data := []byte("revision")
	id := revision.For(data)
	path := filepath.Join(store.revisionsDir, id.String()+".toml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("os.Chmod() failed: %v", err)
	}

	_, err = store.ReadRevision(context.Background(), id)
	if code, ok := CodeOf(err); !ok || code != CodeRevisionCorrupt {
		t.Fatalf("ReadRevision() code = %q, present = %v, want %q; error: %v", code, ok, CodeRevisionCorrupt, err)
	}
}

func TestReadManifestRejectsInsecurePermissions(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	data, err := EncodeManifest(regressionManifest())
	if err != nil {
		t.Fatalf("EncodeManifest() failed: %v", err)
	}
	if err := os.WriteFile(store.manifestPath, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	if err := os.Chmod(store.manifestPath, 0o644); err != nil {
		t.Fatalf("os.Chmod() failed: %v", err)
	}

	_, err = store.ReadManifest(context.Background())
	if code, ok := CodeOf(err); !ok || code != CodeManifestCorrupt {
		t.Fatalf("ReadManifest() code = %q, present = %v, want %q; error: %v", code, ok, CodeManifestCorrupt, err)
	}
}

func TestCleanupTempsReportsRemovalFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	path := filepath.Join(store.root, ".manifest.json.tmp-owned")
	if err := os.WriteFile(path, []byte("temporary"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	if err := os.Chmod(store.root, 0o500); err != nil {
		t.Fatalf("os.Chmod(root) failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.root, 0o700) })

	err = store.CleanupTemps(context.Background())
	if code, ok := CodeOf(err); !ok || code != CodeStoreUnavailable {
		t.Fatalf("CleanupTemps() code = %q, present = %v, want %q; error: %v", code, ok, CodeStoreUnavailable, err)
	}
}
