package configstore

import (
	"context"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

// TestCommitRevisionHoldsLockBetweenRevisionAndManifest lets a second store
// sharing the root try to activate a manifest that does not reference the
// revision being committed, right after the revision is written. With the lock
// held across both steps the second store waits, so it cannot collect the new
// revision before the first store's manifest references it.
func TestCommitRevisionHoldsLockBetweenRevisionAndManifest(t *testing.T) {
	root := t.TempDir()
	first, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dataA, dataX := []byte("a = 1\n"), []byte("x = 9\n")
	idA, idX := revision.For(dataA), revision.For(dataX)
	if err := first.PutRevision(ctx, idA, dataA); err != nil {
		t.Fatal(err)
	}
	if err := first.WriteManifest(ctx, Manifest{FormatVersion: ManifestFormatVersion, Generation: 1, Current: idA, ActivatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	secondDone := make(chan error, 1)
	first.afterRevisionPut = func() {
		go func() {
			secondDone <- second.WriteManifest(ctx, Manifest{FormatVersion: ManifestFormatVersion, Generation: 2, Current: idA, ActivatedAt: time.Now().UTC()})
		}()
		select {
		case err := <-secondDone:
			secondDone <- err
		case <-time.After(300 * time.Millisecond):
		}
	}
	commit := Manifest{FormatVersion: ManifestFormatVersion, Generation: 2, Current: idX, Previous: idA, ActivatedAt: time.Now().UTC()}
	if err := first.CommitRevision(ctx, idX, dataX, commit); err != nil {
		t.Fatalf("CommitRevision() = %v; a concurrent store collected the uncommitted revision", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	manifest, err := first.ReadManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []revision.ID{manifest.Current, manifest.Previous} {
		if id == "" {
			continue
		}
		if _, err := first.ReadRevision(ctx, id); err != nil {
			t.Fatalf("manifest references unreadable revision %s: %v", id, err)
		}
	}
}

func TestCommitRevisionRejectsManifestNotReferencingRevision(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dataA, dataX := []byte("a = 1\n"), []byte("x = 9\n")
	idA, idX := revision.For(dataA), revision.For(dataX)
	if err := store.PutRevision(context.Background(), idA, dataA); err != nil {
		t.Fatal(err)
	}
	err = store.CommitRevision(context.Background(), idX, dataX, Manifest{FormatVersion: ManifestFormatVersion, Generation: 1, Current: idA, ActivatedAt: time.Now().UTC()})
	if code, _ := CodeOf(err); code != CodeInvalidRevision {
		t.Fatalf("CommitRevision() = %v, want %s", err, CodeInvalidRevision)
	}
}

// TestStoreLockWaitHonorsCancellationInProcess blocks one mutation inside the
// lock and verifies a second mutation on the same FileStore stops waiting
// when its context is cancelled instead of blocking until the holder exits.
func TestStoreLockWaitHonorsCancellationInProcess(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("a = 1\n")
	id := revision.For(data)
	if err := store.PutRevision(context.Background(), id, data); err != nil {
		t.Fatal(err)
	}
	held, release := make(chan struct{}), make(chan struct{})
	store.afterManifestVerify = func() {
		close(held)
		<-release
	}
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- store.WriteManifest(context.Background(), Manifest{FormatVersion: ManifestFormatVersion, Generation: 1, Current: id, ActivatedAt: time.Now().UTC()})
	}()
	<-held
	defer func() {
		close(release)
		if err := <-holderDone; err != nil {
			t.Errorf("holder WriteManifest() = %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- store.PutRevision(ctx, id, data) }()
	select {
	case err := <-waiterDone:
		if code, _ := CodeOf(err); code != CodeCancelled {
			t.Fatalf("PutRevision() = %v, want %s", err, CodeCancelled)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("PutRevision() kept waiting for the in-process lock after cancellation")
	}
}
