package config

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/configstore"
	"github.com/abyssbugg/ant-router/internal/revision"
)

// committingStore records whether activation used the atomic commit path.
type committingStore struct {
	*configstore.FileStore
	commits, puts int
}

func (s *committingStore) PutRevision(ctx context.Context, id revision.ID, data []byte) error {
	s.puts++
	return s.FileStore.PutRevision(ctx, id, data)
}

func (s *committingStore) CommitRevision(ctx context.Context, id revision.ID, data []byte, manifest configstore.Manifest) error {
	s.commits++
	return s.FileStore.CommitRevision(ctx, id, data, manifest)
}

func TestActivateUsesAtomicRevisionCommitWhenAvailable(t *testing.T) {
	base, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &committingStore{FileStore: base}
	manager, err := NewManager(store, func() time.Time { return time.Unix(1, 0) })
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewCandidate(Default(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Activate(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if store.commits != 1 || store.puts != 0 {
		t.Fatalf("commits=%d puts=%d; activation must use one atomic CommitRevision", store.commits, store.puts)
	}
	if manifest, err := base.ReadManifest(context.Background()); err != nil || manifest.Current != snapshot.RevisionID() {
		t.Fatalf("manifest=%+v err=%v, want current %s", manifest, err, snapshot.RevisionID())
	}
}

func TestLoadStartupUsesAtomicRevisionCommitForChangedSource(t *testing.T) {
	base, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	writeStartupSource(t, sourcePath, Default())
	seed := seedStartupStore(t, base, sourcePath)
	changed := Default()
	changed.Daemon.ShutdownTimeout = Duration{60 * time.Second}
	writeStartupSource(t, sourcePath, changed)

	store := &committingStore{FileStore: base}
	manager, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.LoadStartup(context.Background(), sourcePath)
	if err != nil {
		t.Fatalf("LoadStartup() failed: %v", err)
	}
	if store.commits != 1 || store.puts != 0 {
		t.Fatalf("commits=%d puts=%d; startup must use one atomic CommitRevision", store.commits, store.puts)
	}
	manifest, err := base.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Current != snapshot.RevisionID() || manifest.Current == seed.RevisionID() || manifest.Previous != seed.RevisionID() {
		t.Fatalf("manifest current/previous = %s/%s, want %s/%s", manifest.Current, manifest.Previous, snapshot.RevisionID(), seed.RevisionID())
	}
}

// fallbackStore embeds only the Store interface, so it does not implement
// RevisionCommitter; it cancels the context once the revision is stored.
type fallbackStore struct {
	configstore.Store
	cancel context.CancelFunc
	puts   int
}

func (s *fallbackStore) PutRevision(ctx context.Context, id revision.ID, data []byte) error {
	s.puts++
	err := s.Store.PutRevision(ctx, id, data)
	s.cancel()
	return err
}

func TestLoadStartupFallbackCancelledBetweenPutAndManifestLeavesManifest(t *testing.T) {
	base, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	writeStartupSource(t, sourcePath, Default())
	seed := seedStartupStore(t, base, sourcePath)
	before, err := base.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed := Default()
	changed.Daemon.ShutdownTimeout = Duration{60 * time.Second}
	writeStartupSource(t, sourcePath, changed)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &fallbackStore{Store: base, cancel: cancel}
	if _, ok := any(store).(configstore.RevisionCommitter); ok {
		t.Fatal("fallbackStore must not implement RevisionCommitter")
	}
	manager, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.LoadStartup(ctx, sourcePath)
	var configErr *Error
	if !errors.As(err, &configErr) || configErr.Code != CodeCancelled {
		t.Fatalf("LoadStartup() error = %v, want %s", err, CodeCancelled)
	}
	if store.puts != 1 {
		t.Fatalf("puts=%d, want fallback PutRevision exactly once", store.puts)
	}
	after, err := base.ReadManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after != before || after.Current != seed.RevisionID() {
		t.Fatalf("manifest changed after cancellation: before=%+v after=%+v", before, after)
	}
	if _, ok := manager.Current(); ok {
		t.Fatal("cancelled startup must not publish a snapshot")
	}
}
