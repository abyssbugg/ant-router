package configstore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

func TestWriteManifestCollectsUnreferencedRevisions(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := []byte("first revision payload")
	second := []byte("second revision payload")
	third := []byte("third revision payload")
	activations := []struct {
		data     []byte
		manifest Manifest
	}{
		{first, Manifest{FormatVersion: ManifestFormatVersion, Generation: 1, Current: revision.For(first), ActivatedAt: time.Unix(1, 0)}},
		{second, Manifest{FormatVersion: ManifestFormatVersion, Generation: 2, Current: revision.For(second), Previous: revision.For(first), ActivatedAt: time.Unix(2, 0)}},
		{third, Manifest{FormatVersion: ManifestFormatVersion, Generation: 3, Current: revision.For(third), Previous: revision.For(second), ActivatedAt: time.Unix(3, 0)}},
	}
	for _, activation := range activations {
		if err := fs.PutRevision(ctx, revision.For(activation.data), activation.data); err != nil {
			t.Fatal(err)
		}
		if err := fs.WriteManifest(ctx, activation.manifest); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(fs.revisionsDir)
	if err != nil {
		t.Fatal(err)
	}
	remaining := make(map[string]bool, len(entries))
	for _, entry := range entries {
		remaining[entry.Name()] = true
	}
	want := map[string]bool{revision.For(second).String() + ".toml": true, revision.For(third).String() + ".toml": true}
	if len(remaining) != len(want) {
		t.Fatalf("remaining revisions = %v, want %v", remaining, want)
	}
	for id := range want {
		if !remaining[id] {
			t.Fatalf("referenced revision %s was collected", id)
		}
	}
	// The store still reads both retained revisions.
	for _, data := range [][]byte{second, third} {
		if _, err := fs.ReadRevision(ctx, revision.For(data)); err != nil {
			t.Fatalf("ReadRevision(%s) failed: %v", revision.For(data), err)
		}
	}
}
