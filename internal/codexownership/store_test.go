package codexownership

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorePublishesCurrentAndPreviousGenerations(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put(context.Background(), Input{ProfilePath: "/home/user/.codex/ant-router.config.toml", CatalogPath: "/state/models.json", Profile: []byte("first-profile"), Catalog: []byte("first-catalog"), CodexVersion: "0.134.0", RouterctlPath: "/usr/bin/routerctl", CreatedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	current, generation, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Current != first.ID || current.Previous != "" || string(generation.Profile) != "first-profile" || string(generation.Catalog) != "first-catalog" {
		t.Fatalf("first state = %#v, %#v", current, generation)
	}
	second, err := store.Put(context.Background(), Input{ProfilePath: current.ProfilePath, CatalogPath: current.CatalogPath, Profile: []byte("second-profile"), Catalog: []byte("second-catalog"), CodexVersion: "0.134.0", RouterctlPath: "/usr/bin/routerctl", CreatedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	current, generation, err = store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Current != second.ID || current.Previous != first.ID || string(generation.Profile) != "second-profile" {
		t.Fatalf("second state = %#v, %#v", current, generation)
	}
	previous, err := store.Generation(context.Background(), first.ID)
	if err != nil || string(previous.Profile) != "first-profile" {
		t.Fatalf("previous = %#v, %v", previous, err)
	}
	generation.Profile[0] = 'X'
	again, err := store.Generation(context.Background(), second.ID)
	if err != nil || string(again.Profile) != "second-profile" {
		t.Fatal("generation bytes aliased caller")
	}
}

func TestStorePutIsIdempotentForIdenticalContent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := Input{ProfilePath: "/home/user/.codex/ant-router.config.toml", CatalogPath: "/state/models.json", Profile: []byte("profile"), Catalog: []byte("catalog"), CodexVersion: "0.134.0", RouterctlPath: "/usr/bin/routerctl", CreatedAt: time.Unix(1, 0)}
	first, err := store.Put(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.CreatedAt = time.Unix(2, 0)
	second, err := store.Put(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || manifest.Current != first.ID || manifest.Previous != "" {
		t.Fatalf("idempotent state = %#v, first=%s second=%s", manifest, first.ID, second.ID)
	}
}

func TestCurrentRejectsDuplicateManifestKeys(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := Input{ProfilePath: "/profile", CatalogPath: "/catalog", Profile: []byte("profile"), Catalog: []byte("catalog"), CodexVersion: "0.153.4", RouterctlPath: "/routerctl", CreatedAt: time.Unix(1, 0)}
	generation, err := store.Put(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, "ownership.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "\"current\": \""+generation.ID+"\"", "\"current\": \""+generation.ID+"\",\n  \"current\": \""+generation.ID+"\"", 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Current(context.Background()); err == nil {
		t.Fatal("duplicate manifest key accepted")
	}
}

func TestStoreRetainsTenNewestGenerations(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var latest Manifest
	for index := 0; index < 12; index++ {
		_, err := store.Put(context.Background(), Input{ProfilePath: "/profile", CatalogPath: "/catalog", Profile: []byte{byte(index + 1)}, Catalog: []byte("catalog"), CodexVersion: "0.153.4", RouterctlPath: "/routerctl", CreatedAt: time.Unix(int64(index+1), 0)})
		if err != nil {
			t.Fatal(err)
		}
		latest, _, err = store.Current(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(store.root, "revisions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Fatalf("revision count = %d, want 10", len(entries))
	}
	if _, err := store.Generation(context.Background(), latest.Current); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Generation(context.Background(), latest.Previous); err != nil {
		t.Fatal(err)
	}
}

func TestNewStoreRejectsSymlinkedRoot(t *testing.T) {
	realRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "state-link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(link); err == nil {
		t.Fatal("symlinked root accepted")
	}
}

func TestGenerationRejectsSymlinkedManagedFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.Put(context.Background(), ownershipInput("profile", 1))
	if err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(store.root, "revisions", generation.ID, "profile.toml")
	if err := os.Remove(profilePath); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secretPath, []byte("secret-material"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPath, profilePath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Generation(context.Background(), generation.ID); err == nil || strings.Contains(err.Error(), "secret-material") {
		t.Fatalf("error = %v", err)
	}
}

func TestStoreRejectsInvalidInputWithoutLeakingBytes(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tests := []Input{
		{},
		{ProfilePath: "relative", CatalogPath: "/catalog", Profile: []byte("secret")},
		{ProfilePath: "/profile", CatalogPath: "/catalog", Profile: make([]byte, MaxProfileBytes+1)},
		{ProfilePath: "/profile", CatalogPath: "/catalog", Profile: []byte("ok"), Catalog: make([]byte, MaxCatalogBytes+1)},
	}
	for _, input := range tests {
		if _, err := store.Put(context.Background(), input); err == nil {
			t.Fatal("expected invalid input error")
		}
	}
}
