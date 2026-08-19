package configstore

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

func TestCleanupTempsSerializesWithPutRevision(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}
	originalWrite := store.writeFile
	writeBlocked := make(chan struct{})
	releaseWrite := make(chan struct{})
	store.writeFile = func(ctx context.Context, path string, data []byte, perm fs.FileMode) error {
		return writeAtomicWithHooks(ctx, path, data, perm, &atomicWriteHooks{
			beforeReplace: func() {
				close(writeBlocked)
				<-releaseWrite
			},
		})
	}
	data := []byte("revision")
	id := revision.For(data)
	putDone := make(chan error, 1)
	go func() { putDone <- store.PutRevision(context.Background(), id, data) }()
	<-writeBlocked

	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- store.CleanupTemps(context.Background()) }()
	select {
	case cleanupErr := <-cleanupDone:
		t.Fatalf("CleanupTemps completed during an active write: %v", cleanupErr)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseWrite)
	if err := <-putDone; err != nil {
		t.Fatalf("PutRevision failed after release: %v", err)
	}
	if err := <-cleanupDone; err != nil {
		t.Fatalf("CleanupTemps failed after write: %v", err)
	}
	store.writeFile = originalWrite
}
