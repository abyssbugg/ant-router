package agentpublication

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestManager(t *testing.T, existing string) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	store, err := NewStore(root, "gemini/ant-router")
	if err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if existing != "" {
		if err := os.WriteFile(settingsPath, []byte(existing), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manager, err := NewManager(store, SettingsFile{Path: settingsPath}, "gemini")
	if err != nil {
		t.Fatal(err)
	}
	return manager, settingsPath
}

func TestPublishMergesAndPreservesUserContent(t *testing.T) {
	existing := `{"theme":"dark","mcpServers":{"a":{"command":"x"}}}`
	manager, settingsPath := newTestManager(t, existing)
	result, err := manager.Publish(context.Background(), sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("publish reported no change")
	}
	published, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(published, &document); err != nil {
		t.Fatal(err)
	}
	// Values survive the merge; whitespace is normalized by re-marshaling.
	if document["theme"] != "dark" {
		t.Fatalf("theme = %#v", document["theme"])
	}
	providers, ok := document["providers"].(map[string]any)
	if !ok || providers["ant-router"] == nil {
		t.Fatalf("providers = %#v", document["providers"])
	}
}

func TestDisableRestoresExactPreImage(t *testing.T) {
	existing := `{"theme":"dark","custom":true}`
	manager, settingsPath := newTestManager(t, existing)
	if _, err := manager.Publish(context.Background(), sampleInput()); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Disable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("disable reported no change")
	}
	restored, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != existing {
		t.Fatalf("restored = %q, want exact pre-image %q", restored, existing)
	}
}

func TestDisableRemovesFileWhenNoneExisted(t *testing.T) {
	manager, settingsPath := newTestManager(t, "")
	if _, err := manager.Publish(context.Background(), sampleInput()); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Disable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("disable reported no change")
	}
	if _, err := os.Lstat(settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings still exist: %v", err)
	}
}

func TestRestoreRepublishesAfterDisable(t *testing.T) {
	manager, settingsPath := newTestManager(t, "")
	if _, err := manager.Publish(context.Background(), sampleInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Restore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("restore reported no change")
	}
	published, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(published), `"ant-router"`) {
		t.Fatalf("restored publication missing entry: %s", published)
	}
}

func TestPublishRejectsConcurrentDrift(t *testing.T) {
	manager, settingsPath := newTestManager(t, `{"theme":"dark"}`)
	// Inject a concurrent writer between the two observations: the first
	// observe returns the pristine pre-image, the second (the re-observation)
	// returns the file after a concurrent user edit. The drift guard must
	// refuse the publication, leaving the user's edit byte-exact on disk.
	observations := 0
	manager.observeFn = func() ([]byte, error) {
		observations++
		if observations == 1 {
			return []byte(`{"theme":"dark"}`), nil
		}
		if err := os.WriteFile(settingsPath, []byte(`{"theme":"dark","userEdit":true}`), 0o600); err != nil {
			return nil, err
		}
		return []byte(`{"theme":"dark","userEdit":true}`), nil
	}
	_, err := manager.Publish(context.Background(), sampleInput())
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("error = %v, want ErrDrift", err)
	}
	onDisk, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(onDisk, &document); err != nil {
		t.Fatal(err)
	}
	if document["userEdit"] != true {
		t.Fatalf("drifted user edit destroyed: %s", onDisk)
	}
}

func TestStatusReportsPublishedAndDrift(t *testing.T) {
	manager, settingsPath := newTestManager(t, "")
	status, err := manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Published {
		t.Fatal("unpublished status = published")
	}
	if _, err := manager.Publish(context.Background(), sampleInput()); err != nil {
		t.Fatal(err)
	}
	status, err = manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Published || status.Drifted {
		t.Fatalf("status = %#v, want published without drift", status)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"userEdit":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Drifted {
		t.Fatal("drift not detected")
	}
}

func TestStoreRejectsCorruptGeneration(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, "gemini/ant-router")
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.Stage(filepath.Join(t.TempDir(), "settings.json"), "gemini", []byte(`{}`), []byte(`{"merged":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.mergedPath(generation.ID), []byte(`{"tampered":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(generation.ID); !errors.Is(err, errStoreCorrupt) {
		t.Fatalf("load error = %v, want errStoreCorrupt", err)
	}
}

func TestDisableRefusesPathMismatch(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, "gemini/ant-router")
	if err != nil {
		t.Fatal(err)
	}
	pathA := filepath.Join(t.TempDir(), "a-settings.json")
	pathB := filepath.Join(t.TempDir(), "b-settings.json")
	if err := os.WriteFile(pathA, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(store, agentSettings(pathA), "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Publish(context.Background(), sampleInput()); err != nil {
		t.Fatal(err)
	}
	managerB, err := NewManager(store, agentSettings(pathB), "gemini")
	if err != nil {
		t.Fatal(err)
	}
	_, err = managerB.Disable(context.Background())
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("cross-path disable error = %v, want ErrDrift", err)
	}
	// Path A must be untouched (still published content).
	onDisk, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "ant-router") {
		t.Fatalf("path A destroyed: %s", onDisk)
	}
}

func TestDisableRefusesDriftedFile(t *testing.T) {
	manager, settingsPath := newTestManager(t, `{"theme":"dark"}`)
	if _, err := manager.Publish(context.Background(), sampleInput()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"userEdit":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Disable(context.Background())
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("error = %v, want ErrDrift", err)
	}
	onDisk, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(onDisk, &document); err != nil {
		t.Fatal(err)
	}
	if document["userEdit"] != true {
		t.Fatalf("user edit destroyed: %s", onDisk)
	}
}

func agentSettings(path string) SettingsFile {
	return SettingsFile{Path: path}
}
