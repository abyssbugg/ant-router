package inferencetoken

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/abyssbugg/ant-router/internal/controltransport"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() failed: %v", err)
	}
	return store
}

func TestNewStoreAndMissingToken(t *testing.T) {
	if _, err := NewStore("  "); CodeOfValue(err) != CodeStoreUnavailable {
		t.Fatalf("blank root code = %q, want %q", CodeOfValue(err), CodeStoreUnavailable)
	}
	store := newTestStore(t)
	if !filepath.IsAbs(store.stateRoot) {
		t.Errorf("stateRoot = %q, want absolute", store.stateRoot)
	}
	_, err := store.Load(controltransport.DefaultInstance)
	if CodeOfValue(err) != CodeTokenNotFound {
		t.Fatalf("Load missing code = %q, want %q", CodeOfValue(err), CodeTokenNotFound)
	}
	paths, pathErr := store.resolveInstance(controltransport.DefaultInstance)
	if pathErr != nil {
		t.Fatalf("resolveInstance() failed: %v", pathErr)
	}
	if _, statErr := os.Stat(paths.StateDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Load() created state directory: %v", statErr)
	}
}

func TestLoadOrCreatePersistsAndReusesToken(t *testing.T) {
	store := newTestStore(t)
	material := bytes.Repeat([]byte{0x21}, TokenSize)
	store.random = bytes.NewReader(material)
	created, err := store.LoadOrCreate(controltransport.DefaultInstance)
	if err != nil {
		t.Fatalf("LoadOrCreate() failed: %v", err)
	}
	store.random = bytes.NewReader(bytes.Repeat([]byte{0x22}, TokenSize))
	reused, err := store.LoadOrCreate(controltransport.DefaultInstance)
	if err != nil {
		t.Fatalf("second LoadOrCreate() failed: %v", err)
	}
	if reused != created {
		t.Error("LoadOrCreate replaced the persistent token")
	}
	loaded, err := store.Load(controltransport.DefaultInstance)
	if err != nil || loaded != created {
		t.Fatalf("Load() = %v, %v", loaded, err)
	}
}

func TestConcurrentLoadOrCreateConverges(t *testing.T) {
	root := t.TempDir()
	const workers = 16
	results := make(chan Token, workers)
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := 0; index < workers; index++ {
		go func(value byte) {
			defer wait.Done()
			store, err := NewStore(root)
			if err != nil {
				errorsFound <- err
				return
			}
			store.random = bytes.NewReader(bytes.Repeat([]byte{value}, TokenSize))
			token, err := store.LoadOrCreate(controltransport.DefaultInstance)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- token
		}(byte(index + 1))
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("LoadOrCreate worker failed: %v", err)
	}
	var expected Token
	first := true
	for token := range results {
		if first {
			expected, first = token, false
		} else if token != expected {
			t.Fatal("concurrent creators returned different tokens")
		}
	}
}

func TestLoadRejectsCorruptTokenWithoutEcho(t *testing.T) {
	store := newTestStore(t)
	paths, _ := store.resolveInstance(controltransport.DefaultInstance)
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	canary := "synthetic_token_marker_24680"
	if err := os.WriteFile(paths.TokenPath, []byte(canary), 0o600); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	_, err := store.Load(controltransport.DefaultInstance)
	if CodeOfValue(err) != CodeTokenCorrupt {
		t.Fatalf("Load corrupt code = %q, want %q", CodeOfValue(err), CodeTokenCorrupt)
	}
	if bytes.Contains([]byte(err.Error()), []byte(canary)) {
		t.Error("error exposed corrupt token material")
	}
}

func TestLoadRejectsOversizedTokenFile(t *testing.T) {
	store := newTestStore(t)
	paths, _ := store.resolveInstance(controltransport.DefaultInstance)
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	if err := os.WriteFile(paths.TokenPath, bytes.Repeat([]byte{'x'}, 1<<20), 0o600); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	if _, err := store.Load(controltransport.DefaultInstance); CodeOfValue(err) != CodeTokenCorrupt {
		t.Fatalf("oversized token code = %q, want %q", CodeOfValue(err), CodeTokenCorrupt)
	}
}

func TestWriteTokenAtomicReportsPostReplaceUncertainty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inference.token")
	oldToken, _ := Generate(bytes.NewReader(bytes.Repeat([]byte{0x51}, TokenSize)))
	newToken, _ := Generate(bytes.NewReader(bytes.Repeat([]byte{0x52}, TokenSize)))
	if err := os.WriteFile(path, []byte(oldToken.String()), 0o600); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	sentinel := errors.New("directory sync failed")
	err := writeTokenAtomicWithHooks(path, []byte(newToken.String()), 0o600, &tokenWriteHooks{
		syncDirectory: func(string) error { return sentinel },
	})
	if code, ok := CodeOf(err); !ok || code != CodeDurabilityUncertain {
		t.Fatalf("code = %q, present = %v, want %q; error: %v", code, ok, CodeDurabilityUncertain, err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("os.ReadFile() failed: %v", readErr)
	}
	if string(data) != newToken.String() {
		t.Error("post-replace uncertainty did not leave the new visible token")
	}
}

func TestRotatePersistsAndFailureKeepsOldToken(t *testing.T) {
	store := newTestStore(t)
	store.random = bytes.NewReader(bytes.Repeat([]byte{0x31}, TokenSize))
	oldToken, err := store.LoadOrCreate(controltransport.DefaultInstance)
	if err != nil {
		t.Fatalf("LoadOrCreate() failed: %v", err)
	}
	store.random = bytes.NewReader(bytes.Repeat([]byte{0x32}, TokenSize))
	rotated, err := store.Rotate(controltransport.DefaultInstance)
	if err != nil {
		t.Fatalf("Rotate() failed: %v", err)
	}
	if rotated == oldToken {
		t.Error("Rotate returned the old token")
	}
	loaded, _ := store.Load(controltransport.DefaultInstance)
	if loaded != rotated {
		t.Error("rotated token was not persisted")
	}

	sentinel := errors.New("write failed")
	store.writeFile = func(string, []byte, fs.FileMode) error { return sentinel }
	store.random = bytes.NewReader(bytes.Repeat([]byte{0x33}, TokenSize))
	if _, err := store.Rotate(controltransport.DefaultInstance); !errors.Is(err, sentinel) {
		t.Fatalf("Rotate failure = %v, want sentinel", err)
	}
	loaded, _ = store.Load(controltransport.DefaultInstance)
	if loaded != rotated {
		t.Error("failed rotation changed the stored token")
	}
}

func CodeOfValue(err error) Code {
	code, _ := CodeOf(err)
	return code
}
