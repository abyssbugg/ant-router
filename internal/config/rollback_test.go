package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/configstore"
)

type rollbackStore struct {
	configstore.Store
	writeErr error
}

func (s *rollbackStore) WriteManifest(ctx context.Context, manifest configstore.Manifest) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	return s.Store.WriteManifest(ctx, manifest)
}

func activateTwoConfigurations(t *testing.T, manager *Manager) (Snapshot, Snapshot) {
	t.Helper()
	firstCandidate, err := NewCandidate(Default(), "", 0)
	if err != nil {
		t.Fatalf("NewCandidate(first) failed: %v", err)
	}
	first, err := manager.Activate(context.Background(), firstCandidate)
	if err != nil {
		t.Fatalf("Activate(first) failed: %v", err)
	}
	secondConfig := Default()
	secondConfig.Daemon.ShutdownTimeout = Duration{60 * time.Second}
	secondCandidate, err := NewCandidate(secondConfig, "", first.Generation())
	if err != nil {
		t.Fatalf("NewCandidate(second) failed: %v", err)
	}
	second, err := manager.Activate(context.Background(), secondCandidate)
	if err != nil {
		t.Fatalf("Activate(second) failed: %v", err)
	}
	return first, second
}

func TestPreviousAndRollbackToggle(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, err := NewManager(store, func() time.Time {
		return time.Date(2026, 8, 19, 6, 0, 0, 0, time.FixedZone("test", 2*60*60))
	})
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}
	first, second := activateTwoConfigurations(t, manager)

	previous, ok, err := manager.Previous(context.Background())
	if err != nil {
		t.Fatalf("Previous() failed: %v", err)
	}
	if !ok || previous != first.RevisionID() {
		t.Fatalf("Previous() = %s, %v, want %s, true", previous, ok, first.RevisionID())
	}

	rolledBack, err := manager.Rollback(context.Background())
	if err != nil {
		t.Fatalf("Rollback() failed: %v", err)
	}
	if rolledBack.Generation() != 3 || rolledBack.RevisionID() != first.RevisionID() {
		t.Errorf("rollback generation/revision = %d/%s, want 3/%s", rolledBack.Generation(), rolledBack.RevisionID(), first.RevisionID())
	}
	if rolledBack.Config().Daemon.ShutdownTimeout.Duration != 30*time.Second {
		t.Errorf("rollback timeout = %s, want 30s", rolledBack.Config().Daemon.ShutdownTimeout)
	}
	if rolledBack.ActivatedAt().Location() != time.UTC {
		t.Errorf("rollback timestamp location = %v, want UTC", rolledBack.ActivatedAt().Location())
	}
	manifest, err := store.ReadManifest(context.Background())
	if err != nil {
		t.Fatalf("ReadManifest() failed: %v", err)
	}
	if manifest.Current != first.RevisionID() || manifest.Previous != second.RevisionID() || manifest.Generation != 3 {
		t.Errorf("rollback manifest = current %s previous %s generation %d", manifest.Current, manifest.Previous, manifest.Generation)
	}

	rolledForward, err := manager.Rollback(context.Background())
	if err != nil {
		t.Fatalf("second Rollback() failed: %v", err)
	}
	if rolledForward.Generation() != 4 || rolledForward.RevisionID() != second.RevisionID() {
		t.Errorf("second rollback = generation %d revision %s, want 4/%s", rolledForward.Generation(), rolledForward.RevisionID(), second.RevisionID())
	}
}

func TestRollbackWithoutPreviousDoesNotPublish(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)
	candidate, _ := NewCandidate(Default(), "", 0)
	active, err := manager.Activate(context.Background(), candidate)
	if err != nil {
		t.Fatalf("Activate() failed: %v", err)
	}

	_, err = manager.Rollback(context.Background())
	if code, ok := CodeOf(err); !ok || code != CodeNoPreviousRevision {
		t.Fatalf("Rollback() code = %q, present = %v, want %q", code, ok, CodeNoPreviousRevision)
	}
	current, _ := manager.Current()
	if current.Generation() != active.Generation() || current.RevisionID() != active.RevisionID() {
		t.Error("failed rollback changed the active snapshot")
	}
}

func TestRollbackRejectsManifestMemoryMismatch(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)
	first, second := activateTwoConfigurations(t, manager)
	manifest, _ := store.ReadManifest(context.Background())
	manifest.Generation++
	manifest.Current, manifest.Previous = first.RevisionID(), second.RevisionID()
	if err := store.WriteManifest(context.Background(), manifest); err != nil {
		t.Fatalf("WriteManifest(mismatch) failed: %v", err)
	}

	_, err = manager.Rollback(context.Background())
	if code, ok := CodeOf(err); !ok || code != CodeValidationFailed {
		t.Fatalf("Rollback() code = %q, present = %v, want %q", code, ok, CodeValidationFailed)
	}
	current, _ := manager.Current()
	if current.RevisionID() != second.RevisionID() {
		t.Error("manifest mismatch changed the active snapshot")
	}
}

func TestRollbackRejectsCorruptPreviousRevision(t *testing.T) {
	root := t.TempDir()
	store, err := configstore.NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)
	first, second := activateTwoConfigurations(t, manager)
	path := filepath.Join(root, "revisions", first.RevisionID().String()+".toml")
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt) failed: %v", err)
	}

	_, err = manager.Rollback(context.Background())
	if code, ok := configstore.CodeOf(err); !ok || code != configstore.CodeRevisionCorrupt {
		t.Fatalf("Rollback() store code = %q, present = %v, want %q", code, ok, configstore.CodeRevisionCorrupt)
	}
	current, _ := manager.Current()
	if current.RevisionID() != second.RevisionID() {
		t.Error("corrupt rollback changed the active snapshot")
	}
}

func TestRollbackWriteFailureDoesNotPublish(t *testing.T) {
	base, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	store := &rollbackStore{Store: base}
	manager, _ := NewManager(store, time.Now)
	_, second := activateTwoConfigurations(t, manager)
	sentinel := &configstore.Error{Code: configstore.CodeDurabilityUncertain, Err: errors.New("sync failed")}
	store.writeErr = sentinel

	_, err = manager.Rollback(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Rollback() error = %v, want sentinel", err)
	}
	current, _ := manager.Current()
	if current.RevisionID() != second.RevisionID() {
		t.Error("failed manifest write changed the active snapshot")
	}
}

func TestRollbackAndPreviousRespectCancellation(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, _ := NewManager(store, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := manager.Previous(ctx); codeOfConfig(err) != CodeCancelled {
		t.Fatalf("Previous(cancelled) code = %q, want %q", codeOfConfig(err), CodeCancelled)
	}
	if _, err := manager.Rollback(ctx); codeOfConfig(err) != CodeCancelled {
		t.Fatalf("Rollback(cancelled) code = %q, want %q", codeOfConfig(err), CodeCancelled)
	}
}

func codeOfConfig(err error) Code {
	code, _ := CodeOf(err)
	return code
}
