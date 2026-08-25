package configservice

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/configstore"
)

func canonicalConfig(t *testing.T, timeout time.Duration) []byte {
	t.Helper()
	cfg := config.Default()
	cfg.Daemon.ShutdownTimeout = config.Duration{Duration: timeout}
	data, err := config.Canonical(cfg)
	if err != nil {
		t.Fatalf("Canonical() failed: %v", err)
	}
	return data
}

func newService(t *testing.T, root string) (*Service, *configstore.FileStore) {
	t.Helper()
	store, err := configstore.NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	manager, err := config.NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}
	service, err := New(manager, store)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	return service, store
}

func TestInitializeStates(t *testing.T) {
	t.Run("awaiting", func(t *testing.T) {
		service, _ := newService(t, t.TempDir())
		state, err := service.Initialize(context.Background())
		if err != nil || state != StateAwaitingConfiguration {
			t.Fatalf("Initialize() = %q, %v", state, err)
		}
	})

	t.Run("running", func(t *testing.T) {
		root := t.TempDir()
		seed, _ := newService(t, root)
		if _, err := seed.Initialize(context.Background()); err != nil {
			t.Fatalf("seed Initialize() failed: %v", err)
		}
		validation, err := seed.Validate(context.Background(), canonicalConfig(t, 30*time.Second))
		if err != nil {
			t.Fatalf("Validate() failed: %v", err)
		}
		if _, err := seed.Apply(context.Background(), ApplyRequest{TOML: canonicalConfig(t, 30*time.Second), ExpectedRevision: validation.Revision, ExpectedGeneration: validation.BaseGeneration}); err != nil {
			t.Fatalf("Apply() failed: %v", err)
		}
		restarted, _ := newService(t, root)
		state, err := restarted.Initialize(context.Background())
		if err != nil || state != StateRunning {
			t.Fatalf("restart Initialize() = %q, %v", state, err)
		}
	})
}

func TestValidateIsWriteFree(t *testing.T) {
	service, store := newService(t, t.TempDir())
	_, _ = service.Initialize(context.Background())
	data := canonicalConfig(t, 30*time.Second)
	result, err := service.Validate(context.Background(), data)
	if err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	if !result.Revision.Valid() || result.BaseGeneration != 0 || result.CanonicalSize == 0 {
		t.Errorf("result = %#v", result)
	}
	if _, err := store.ReadManifest(context.Background()); storeCode(err) != configstore.CodeManifestNotFound {
		t.Fatalf("Validate wrote manifest: %v", err)
	}
}

func TestValidateRejectsInvalidAndOversizedContent(t *testing.T) {
	service, _ := newService(t, t.TempDir())
	_, _ = service.Initialize(context.Background())
	if _, err := service.Validate(context.Background(), []byte("not TOML")); CodeValue(err) != CodeConfigurationInvalid {
		t.Fatalf("invalid code = %q", CodeValue(err))
	}
	if _, err := service.Validate(context.Background(), bytes.Repeat([]byte{'x'}, config.MaxSourceBytes+1)); CodeValue(err) != CodeConfigurationTooLarge {
		t.Fatalf("oversized code = %q", CodeValue(err))
	}
}

func TestApplyProofAndCurrent(t *testing.T) {
	service, _ := newService(t, t.TempDir())
	_, _ = service.Initialize(context.Background())
	data := canonicalConfig(t, 30*time.Second)
	validation, _ := service.Validate(context.Background(), data)
	applied, err := service.Apply(context.Background(), ApplyRequest{TOML: data, ExpectedRevision: validation.Revision, ExpectedGeneration: validation.BaseGeneration})
	if err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}
	if applied.State != StateRunning || applied.Generation != 1 || applied.Revision != validation.Revision {
		t.Errorf("applied = %#v", applied)
	}
	current := service.Current()
	if current.State != StateRunning || current.Generation != 1 || current.Revision != validation.Revision {
		t.Errorf("current = %#v", current)
	}
}

func TestApplyRejectsRevisionAndGenerationConflicts(t *testing.T) {
	service, _ := newService(t, t.TempDir())
	_, _ = service.Initialize(context.Background())
	data := canonicalConfig(t, 30*time.Second)
	validation, _ := service.Validate(context.Background(), data)
	wrong := validation.Revision
	wrong = config.RevisionID(strings.Repeat("a", 64))
	if wrong == validation.Revision {
		wrong = config.RevisionID(strings.Repeat("b", 64))
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{TOML: data, ExpectedRevision: wrong, ExpectedGeneration: 0}); CodeValue(err) != CodeRevisionMismatch {
		t.Fatalf("revision mismatch code = %q", CodeValue(err))
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{TOML: data, ExpectedRevision: validation.Revision, ExpectedGeneration: 1}); CodeValue(err) != CodeGenerationConflict {
		t.Fatalf("generation conflict code = %q", CodeValue(err))
	}
}

func TestRollbackAndNoPrevious(t *testing.T) {
	service, _ := newService(t, t.TempDir())
	_, _ = service.Initialize(context.Background())
	if _, err := service.Rollback(context.Background()); CodeValue(err) != CodeNoPreviousRevision {
		t.Fatalf("initial rollback code = %q", CodeValue(err))
	}
	firstData := canonicalConfig(t, 30*time.Second)
	firstValidation, _ := service.Validate(context.Background(), firstData)
	_, _ = service.Apply(context.Background(), ApplyRequest{TOML: firstData, ExpectedRevision: firstValidation.Revision, ExpectedGeneration: 0})
	secondData := canonicalConfig(t, 60*time.Second)
	secondValidation, _ := service.Validate(context.Background(), secondData)
	second, _ := service.Apply(context.Background(), ApplyRequest{TOML: secondData, ExpectedRevision: secondValidation.Revision, ExpectedGeneration: 1})
	rolled, err := service.Rollback(context.Background())
	if err != nil || rolled.Generation != 3 || rolled.Revision == second.Revision {
		t.Fatalf("Rollback() = %#v, %v", rolled, err)
	}
}

func TestConcurrentApplyOneGenerationConflict(t *testing.T) {
	service, _ := newService(t, t.TempDir())
	_, _ = service.Initialize(context.Background())
	first := canonicalConfig(t, 30*time.Second)
	second := canonicalConfig(t, 60*time.Second)
	firstValidation, _ := service.Validate(context.Background(), first)
	secondValidation, _ := service.Validate(context.Background(), second)
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, err := service.Apply(context.Background(), ApplyRequest{TOML: first, ExpectedRevision: firstValidation.Revision, ExpectedGeneration: 0})
		results <- err
	}()
	go func() {
		defer wait.Done()
		_, err := service.Apply(context.Background(), ApplyRequest{TOML: second, ExpectedRevision: secondValidation.Revision, ExpectedGeneration: 0})
		results <- err
	}()
	wait.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if CodeValue(err) == CodeGenerationConflict {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func storeCode(err error) configstore.Code {
	code, _ := configstore.CodeOf(err)
	return code
}

func CodeValue(err error) Code {
	code, _ := CodeOf(err)
	return code
}
