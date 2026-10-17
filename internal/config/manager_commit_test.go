package config

import (
	"context"
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
