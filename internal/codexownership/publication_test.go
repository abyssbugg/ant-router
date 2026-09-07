package codexownership

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
)

func ownershipInput(profile string, created int64) Input {
	return Input{ProfilePath: "/profile", CatalogPath: "/catalog", Profile: []byte(profile), Catalog: []byte("catalog"), CodexVersion: "0.153.4", RouterctlPath: "/routerctl", CreatedAt: time.Unix(created, 0)}
}

func TestStorePublishesFilesInDeterministicManifestLastOrder(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := store.write
	var names []string
	store.write = func(ctx context.Context, path string, data []byte, mode fs.FileMode) (atomicfile.Result, error) {
		names = append(names, filepath.Base(path))
		return original(ctx, path, data, mode)
	}
	if _, err := store.Put(context.Background(), ownershipInput("profile", 1)); err != nil {
		t.Fatal(err)
	}
	want := []string{"profile.toml", "models.json", "generation.json", "ownership.json"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("write order = %v, want %v", names, want)
	}
}

func TestStoreReportsCommittedManifestDurabilityFailure(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := store.write
	injected := errors.New("directory sync failed")
	store.write = func(ctx context.Context, path string, data []byte, mode fs.FileMode) (atomicfile.Result, error) {
		result, err := original(ctx, path, data, mode)
		if err == nil && filepath.Base(path) == "ownership.json" {
			return result, injected
		}
		return result, err
	}
	generation, err := store.Put(context.Background(), ownershipInput("committed", 1))
	if err == nil || generation.ID == "" || !Committed(err) {
		t.Fatalf("put = %#v, %v", generation, err)
	}
	manifest, current, readErr := store.Current(context.Background())
	if readErr != nil || manifest.Current != generation.ID || current.ID != generation.ID {
		t.Fatalf("committed state = %#v, %#v, %v", manifest, current, readErr)
	}
}

func TestStageRepairsPartialGenerationWithoutPublishing(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{ProfilePath: "/profile", CatalogPath: "/catalog", Profile: []byte("profile"), Catalog: []byte("catalog"), CodexVersion: "0.153.4", RouterctlPath: "/routerctl", CreatedAt: time.Unix(1, 0)}
	id := generationID(input)
	generationRoot := filepath.Join(root, "codex", "ant-router", "revisions", id)
	if err := os.MkdirAll(generationRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generationRoot, "profile.toml"), input.Profile, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := store.Stage(context.Background(), input)
	if err != nil || staged.ID != id || string(staged.Catalog) != "catalog" {
		t.Fatalf("staged=%#v err=%v", staged, err)
	}
	if _, _, err := store.Current(context.Background()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial repair published ownership: %v", err)
	}
}

func TestStageDoesNotPublishUntilCommit(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	firstInput := Input{ProfilePath: "/profile", CatalogPath: "/catalog", Profile: []byte("first-profile"), Catalog: []byte("first-catalog"), CodexVersion: "0.153.4", RouterctlPath: "/routerctl", CreatedAt: time.Unix(1, 0)}
	first, err := store.Put(context.Background(), firstInput)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := Input{ProfilePath: "/profile", CatalogPath: "/catalog", Profile: []byte("second-profile"), Catalog: []byte("second-catalog"), CodexVersion: "0.153.4", RouterctlPath: "/routerctl", CreatedAt: time.Unix(2, 0)}
	staged, err := store.Stage(context.Background(), secondInput)
	if err != nil {
		t.Fatal(err)
	}
	_, current, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != first.ID || staged.ID == first.ID {
		t.Fatalf("current=%s staged=%s", current.ID, staged.ID)
	}
	if _, err := store.Generation(context.Background(), staged.ID); err != nil {
		t.Fatalf("staged generation unavailable: %v", err)
	}
	committed, err := store.Commit(context.Background(), staged.ID)
	if err != nil {
		t.Fatal(err)
	}
	if committed.ID != staged.ID {
		t.Fatalf("committed=%s staged=%s", committed.ID, staged.ID)
	}
	manifest, current, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != staged.ID || manifest.Previous != first.ID {
		t.Fatalf("manifest=%#v current=%s", manifest, current.ID)
	}
}

func TestStoreFailureBeforeManifestPreservesCurrentGeneration(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put(context.Background(), ownershipInput("first", 1))
	if err != nil {
		t.Fatal(err)
	}
	original := store.write
	store.write = func(ctx context.Context, path string, data []byte, mode fs.FileMode) (atomicfile.Result, error) {
		if filepath.Base(path) == "models.json" {
			return atomicfile.Result{}, errors.New("injected")
		}
		return original(ctx, path, data, mode)
	}
	if _, err := store.Put(context.Background(), ownershipInput("second", 2)); err == nil {
		t.Fatal("expected publication failure")
	}
	manifest, current, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Current != first.ID || string(current.Profile) != "first" {
		t.Fatalf("state changed: %#v, %q", manifest, current.Profile)
	}
}
