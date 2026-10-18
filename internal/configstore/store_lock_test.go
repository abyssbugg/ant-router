package configstore

import (
	"context"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

// Two stores on one root model two processes. While the first has verified
// its manifest's revision but not yet written the manifest, the second must
// not be able to activate a different revision and collect the first one.
func TestWriteManifestCoordinatesStoresSharingRoot(t *testing.T) {
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
	// The second manifest references only B, so the second store collects A.
	// Without coordination it could do so after the first store verified A
	// but before the first store published its manifest referencing A.
	dataA, dataB := []byte("a = 1\n"), []byte("b = 2\n")
	idA, idB := revision.For(dataA), revision.For(dataB)
	for _, put := range []struct {
		id   revision.ID
		data []byte
	}{{idA, dataA}, {idB, dataB}} {
		if err := first.PutRevision(ctx, put.id, put.data); err != nil {
			t.Fatal(err)
		}
	}

	secondDone := make(chan error, 1)
	first.afterManifestVerify = func() {
		go func() {
			secondDone <- second.WriteManifest(ctx, Manifest{FormatVersion: ManifestFormatVersion, Generation: 2, Current: idB, ActivatedAt: time.Now().UTC()})
		}()
		select {
		case err := <-secondDone:
			secondDone <- err
			t.Errorf("second store activated while the first held a verified manifest (err=%v)", err)
		case <-time.After(300 * time.Millisecond):
		}
	}
	if err := first.WriteManifest(ctx, Manifest{FormatVersion: ManifestFormatVersion, Generation: 1, Current: idA, Previous: idB, ActivatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	manifest, err := first.ReadManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Current != idB || manifest.Previous != "" {
		t.Fatalf("final manifest = %s/%s, want second store's %s", manifest.Current, manifest.Previous, idB)
	}
	for _, id := range []revision.ID{manifest.Current} {
		if _, err := first.ReadRevision(ctx, id); err != nil {
			t.Fatalf("manifest references unreadable revision %s: %v", id, err)
		}
	}
}

func TestStoreLockHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	holder, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	contender, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := holder.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = contender.CleanupTemps(ctx)
	if code, _ := CodeOf(err); code != CodeCancelled {
		t.Fatalf("CleanupTemps while locked = %v, want CodeCancelled", err)
	}
}
