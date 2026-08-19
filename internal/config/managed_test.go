package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/configstore"
)

func TestLoadManagedStartupNoConfiguration(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)
	_, err = manager.LoadManagedStartup(context.Background())
	if code, ok := CodeOf(err); !ok || code != CodeNoConfiguration {
		t.Fatalf("code = %q, present = %v, want %q; error: %v", code, ok, CodeNoConfiguration, err)
	}
	if _, ok := manager.Current(); ok {
		t.Error("missing managed startup published a snapshot")
	}
}

func TestLoadManagedStartupRestoresActiveSnapshot(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	seed, _ := NewManager(store, func() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) })
	candidate, _ := NewCandidate(Default(), "", 0)
	active, err := seed.Activate(context.Background(), candidate)
	if err != nil {
		t.Fatalf("seed Activate() failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)
	restored, err := manager.LoadManagedStartup(context.Background())
	if err != nil {
		t.Fatalf("LoadManagedStartup() failed: %v", err)
	}
	if restored.RevisionID() != active.RevisionID() || restored.Generation() != active.Generation() || restored.Status() != SnapshotStatusActive || restored.SourceError() != nil || restored.SourcePath() != "" {
		t.Errorf("restored snapshot = revision %s generation %d status %q source %q error %v", restored.RevisionID(), restored.Generation(), restored.Status(), restored.SourcePath(), restored.SourceError())
	}
}

func TestLoadManagedStartupRejectsCorruptState(t *testing.T) {
	t.Run("manifest", func(t *testing.T) {
		root := t.TempDir()
		store, _ := configstore.NewFileStore(root)
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("corrupt"), 0o600); err != nil {
			t.Fatalf("os.WriteFile() failed: %v", err)
		}
		manager, _ := NewManager(store, time.Now)
		_, err := manager.LoadManagedStartup(context.Background())
		if code, ok := configstore.CodeOf(err); !ok || code != configstore.CodeManifestCorrupt {
			t.Fatalf("manifest code = %q, present = %v; error: %v", code, ok, err)
		}
	})

	t.Run("revision", func(t *testing.T) {
		root := t.TempDir()
		store, _ := configstore.NewFileStore(root)
		seed, _ := NewManager(store, time.Now)
		candidate, _ := NewCandidate(Default(), "", 0)
		active, err := seed.Activate(context.Background(), candidate)
		if err != nil {
			t.Fatalf("seed Activate() failed: %v", err)
		}
		path := filepath.Join(root, "revisions", active.RevisionID().String()+".toml")
		if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
			t.Fatalf("os.WriteFile() failed: %v", err)
		}
		manager, _ := NewManager(store, time.Now)
		_, err = manager.LoadManagedStartup(context.Background())
		if code, ok := configstore.CodeOf(err); !ok || code != configstore.CodeRevisionCorrupt {
			t.Fatalf("revision code = %q, present = %v; error: %v", code, ok, err)
		}
	})
}

func TestLoadManagedStartupCancellationAndSingleUse(t *testing.T) {
	store, _ := configstore.NewFileStore(t.TempDir())
	manager, _ := NewManager(store, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.LoadManagedStartup(ctx); codeOfConfig(err) != CodeCancelled {
		t.Fatalf("cancelled code = %q, want %q", codeOfConfig(err), CodeCancelled)
	}
	candidate, _ := NewCandidate(Default(), "", 0)
	if _, err := manager.Activate(context.Background(), candidate); err != nil {
		t.Fatalf("Activate() failed: %v", err)
	}
	if _, err := manager.LoadManagedStartup(context.Background()); codeOfConfig(err) != CodeValidationFailed {
		t.Fatalf("second startup code = %q, want %q", codeOfConfig(err), CodeValidationFailed)
	}
}
