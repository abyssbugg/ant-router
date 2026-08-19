package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/configstore"
)

func writeStartupSource(t *testing.T, path string, cfg Config) {
	t.Helper()
	data, err := Canonical(cfg)
	if err != nil {
		t.Fatalf("Canonical() failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile(source) failed: %v", err)
	}
}

func seedStartupStore(t *testing.T, store configstore.Store, sourcePath string) Snapshot {
	t.Helper()
	manager, err := NewManager(store, func() time.Time {
		return time.Date(2026, 8, 19, 6, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("NewManager(seed) failed: %v", err)
	}
	candidate, err := LoadCandidate(sourcePath, 0)
	if err != nil {
		t.Fatalf("LoadCandidate(seed) failed: %v", err)
	}
	snapshot, err := manager.Activate(context.Background(), candidate)
	if err != nil {
		t.Fatalf("Activate(seed) failed: %v", err)
	}
	return snapshot
}

func TestLoadStartupValidSourceCreatesInitialState(t *testing.T) {
	root := t.TempDir()
	store, err := configstore.NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	writeStartupSource(t, sourcePath, Default())
	manager, _ := NewManager(store, func() time.Time {
		return time.Date(2026, 8, 19, 8, 0, 0, 0, time.FixedZone("test", 2*60*60))
	})

	snapshot, err := manager.LoadStartup(context.Background(), sourcePath)
	if err != nil {
		t.Fatalf("LoadStartup() failed: %v", err)
	}
	if snapshot.Status() != SnapshotStatusActive || snapshot.Generation() != 1 || snapshot.SourceError() != nil {
		t.Errorf("startup snapshot = status %q generation %d sourceErr %v", snapshot.Status(), snapshot.Generation(), snapshot.SourceError())
	}
	if snapshot.ActivatedAt().Location() != time.UTC {
		t.Errorf("startup timestamp location = %v, want UTC", snapshot.ActivatedAt().Location())
	}
	manifest, err := store.ReadManifest(context.Background())
	if err != nil {
		t.Fatalf("ReadManifest() failed: %v", err)
	}
	if manifest.Current != snapshot.RevisionID() || manifest.Generation != 1 {
		t.Errorf("manifest current/generation = %s/%d", manifest.Current, manifest.Generation)
	}
}

func TestLoadStartupMatchingSourceReusesManifest(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	writeStartupSource(t, sourcePath, Default())
	seed := seedStartupStore(t, store, sourcePath)
	manager, _ := NewManager(store, func() time.Time { return seed.ActivatedAt().Add(time.Hour) })

	snapshot, err := manager.LoadStartup(context.Background(), sourcePath)
	if err != nil {
		t.Fatalf("LoadStartup() failed: %v", err)
	}
	if snapshot.Generation() != seed.Generation() || !snapshot.ActivatedAt().Equal(seed.ActivatedAt()) {
		t.Errorf("matching startup changed generation/time: %d/%s", snapshot.Generation(), snapshot.ActivatedAt())
	}
}

func TestLoadStartupChangedSourceCreatesNextGeneration(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	writeStartupSource(t, sourcePath, Default())
	seed := seedStartupStore(t, store, sourcePath)
	changed := Default()
	changed.Daemon.ShutdownTimeout = Duration{60 * time.Second}
	writeStartupSource(t, sourcePath, changed)
	manager, _ := NewManager(store, time.Now)

	snapshot, err := manager.LoadStartup(context.Background(), sourcePath)
	if err != nil {
		t.Fatalf("LoadStartup() failed: %v", err)
	}
	manifest, _ := store.ReadManifest(context.Background())
	if snapshot.Generation() != 2 || manifest.Previous != seed.RevisionID() {
		t.Errorf("changed startup generation/previous = %d/%s", snapshot.Generation(), manifest.Previous)
	}
}

func TestLoadStartupMissingSourceUsesLastKnownGood(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	seedPath := filepath.Join(t.TempDir(), "seed.toml")
	writeStartupSource(t, seedPath, Default())
	seed := seedStartupStore(t, store, seedPath)
	relativeDirectory, err := os.MkdirTemp(".", "tmp-startup-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(relativeDirectory) })
	missingPath := filepath.Join(relativeDirectory, "missing.toml")
	absoluteMissingPath, err := filepath.Abs(missingPath)
	if err != nil {
		t.Fatalf("filepath.Abs() failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)

	snapshot, err := manager.LoadStartup(context.Background(), missingPath)
	if err != nil {
		t.Fatalf("LoadStartup() failed: %v", err)
	}
	if snapshot.Status() != SnapshotStatusLastKnownGood || snapshot.RevisionID() != seed.RevisionID() {
		t.Errorf("LKG snapshot status/revision = %q/%s", snapshot.Status(), snapshot.RevisionID())
	}
	if snapshot.SourcePath() != absoluteMissingPath {
		t.Errorf("LKG SourcePath() = %q, want %q", snapshot.SourcePath(), absoluteMissingPath)
	}
	if code, ok := CodeOf(snapshot.SourceError()); !ok || code != CodeSourceNotFound {
		t.Errorf("SourceError code = %q, present = %v, want %q", code, ok, CodeSourceNotFound)
	}
}

func TestLoadStartupInvalidSourceUsesLastKnownGood(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	writeStartupSource(t, sourcePath, Default())
	seedStartupStore(t, store, sourcePath)
	if err := os.WriteFile(sourcePath, []byte("not valid TOML"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(invalid) failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)

	snapshot, err := manager.LoadStartup(context.Background(), sourcePath)
	if err != nil {
		t.Fatalf("LoadStartup() failed: %v", err)
	}
	if snapshot.Status() != SnapshotStatusLastKnownGood || snapshot.SourceError() == nil {
		t.Errorf("invalid-source recovery status/error = %q/%v", snapshot.Status(), snapshot.SourceError())
	}
}

func TestLoadStartupValidSourceReconstructsCorruptManifest(t *testing.T) {
	root := t.TempDir()
	store, err := configstore.NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	writeStartupSource(t, sourcePath, Default())
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt manifest) failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)

	snapshot, err := manager.LoadStartup(context.Background(), sourcePath)
	if err != nil {
		t.Fatalf("LoadStartup() failed: %v", err)
	}
	if snapshot.Generation() != 1 || snapshot.Status() != SnapshotStatusActive {
		t.Errorf("reconstructed snapshot = generation %d status %q", snapshot.Generation(), snapshot.Status())
	}
}

func TestLoadStartupFailsWithoutValidSourceOrRevision(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)
	missingPath := filepath.Join(t.TempDir(), "missing.toml")

	_, err = manager.LoadStartup(context.Background(), missingPath)
	if code, ok := CodeOf(err); !ok || code != CodeNoValidConfiguration {
		t.Fatalf("LoadStartup() code = %q, present = %v, want %q; error: %v", code, ok, CodeNoValidConfiguration, err)
	}
}

func TestLoadStartupRejectsCorruptLastKnownGoodRevision(t *testing.T) {
	root := t.TempDir()
	store, err := configstore.NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	seedPath := filepath.Join(t.TempDir(), "seed.toml")
	writeStartupSource(t, seedPath, Default())
	seed := seedStartupStore(t, store, seedPath)
	if err := os.WriteFile(filepath.Join(root, "revisions", seed.RevisionID().String()+".toml"), []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt revision) failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)

	_, err = manager.LoadStartup(context.Background(), filepath.Join(t.TempDir(), "missing.toml"))
	if code, ok := CodeOf(err); !ok || code != CodeNoValidConfiguration {
		t.Fatalf("LoadStartup() code = %q, present = %v, want %q; error: %v", code, ok, CodeNoValidConfiguration, err)
	}
}

func TestLoadStartupCleansTempsAndRejectsSecondCall(t *testing.T) {
	root := t.TempDir()
	store, err := configstore.NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	tempPath := filepath.Join(root, ".manifest.json.tmp-startup")
	if err := os.WriteFile(tempPath, []byte("temporary"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(temp) failed: %v", err)
	}
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	writeStartupSource(t, sourcePath, Default())
	manager, _ := NewManager(store, time.Now)

	if _, err := manager.LoadStartup(context.Background(), sourcePath); err != nil {
		t.Fatalf("first LoadStartup() failed: %v", err)
	}
	if _, err := os.Lstat(tempPath); !os.IsNotExist(err) {
		t.Errorf("startup temp still exists: %v", err)
	}
	_, err = manager.LoadStartup(context.Background(), sourcePath)
	if code, ok := CodeOf(err); !ok || code != CodeValidationFailed {
		t.Fatalf("second LoadStartup() code = %q, present = %v, want %q", code, ok, CodeValidationFailed)
	}
}
