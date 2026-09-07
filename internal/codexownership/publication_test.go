package codexownership

import (
	"context"
	"errors"
	"io/fs"
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
