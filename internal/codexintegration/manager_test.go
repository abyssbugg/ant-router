package codexintegration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
	"github.com/abyssbugg/ant-router/internal/codexownership"
	"github.com/abyssbugg/ant-router/internal/control"
)

func newTestManager(t *testing.T) (*Manager, *Planner) {
	t.Helper()
	planner, _ := newTestPlanner(t)
	store := planner.ownership.(*codexownership.Store)
	manager, err := NewManager(planner, store, func() time.Time { return time.Unix(10, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	return manager, planner
}

func TestManagerEnablePublishesExactBytesAndOwnership(t *testing.T) {
	manager, planner := newTestManager(t)
	plan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Enable(context.Background(), plan.Digest())
	if err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string][]byte{plan.input.ProfilePath: plan.Profile(), plan.input.CatalogPath: plan.Catalog()} {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(expected) {
			t.Fatalf("%s = %q, %v", path, actual, err)
		}
	}
	_, current, err := planner.ownership.Current(context.Background())
	if err != nil || current.ID != result.GenerationID {
		t.Fatalf("current=%s result=%s err=%v", current.ID, result.GenerationID, err)
	}
}

type failOnceCommitStore struct {
	*codexownership.Store
	fail bool
}

func (store *failOnceCommitStore) Commit(ctx context.Context, id string) (codexownership.Generation, error) {
	if store.fail {
		store.fail = false
		return codexownership.Generation{}, errors.New("injected ownership commit failure")
	}
	return store.Store.Commit(ctx, id)
}

func TestManagerRecoversFullyPublishedTargetAfterCommitFailure(t *testing.T) {
	_, planner := newTestManager(t)
	base := planner.ownership.(*codexownership.Store)
	failing := &failOnceCommitStore{Store: base, fail: true}
	manager, err := NewManager(planner, failing, func() time.Time { return time.Unix(10, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enable(context.Background(), plan.Digest()); err == nil {
		t.Fatal("ownership commit failure not reported")
	}
	for path, expected := range map[string][]byte{plan.input.ProfilePath: plan.Profile(), plan.input.CatalogPath: plan.Catalog()} {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(expected) {
			t.Fatalf("%s = %q, %v", path, actual, err)
		}
	}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, current, err := base.Current(context.Background())
	if err != nil || current.ID == "" {
		t.Fatalf("current=%s err=%v", current.ID, err)
	}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatalf("idempotent recovery failed: %v", err)
	}
}

func TestManagerRecoversCommittedDisableRemovalUncertainty(t *testing.T) {
	manager, planner := newTestManager(t)
	enable, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enable(context.Background(), enable.Digest()); err != nil {
		t.Fatal(err)
	}
	disable, err := planner.PlanDisable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	original := manager.remove
	injected := errors.New("injected directory sync failure")
	calls := 0
	manager.remove = func(ctx context.Context, path string) (atomicfile.Result, error) {
		calls++
		result, err := original(ctx, path)
		if calls == 1 && err == nil {
			return result, injected
		}
		return result, err
	}
	result, err := manager.Disable(context.Background(), disable.Digest())
	if err == nil || !result.Changed {
		t.Fatalf("disable=%#v err=%v", result, err)
	}
	manager.remove = original
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{enable.input.ProfilePath, enable.input.CatalogPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("file remains: %s", path)
		}
	}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatalf("idempotent recovery failed: %v", err)
	}
}

func TestManagerRecoversPartialFirstEnable(t *testing.T) {
	manager, planner := newTestManager(t)
	plan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	original := manager.write
	calls := 0
	manager.write = func(ctx context.Context, path string, data []byte, mode os.FileMode) (atomicfile.Result, error) {
		calls++
		if calls == 3 {
			return atomicfile.Result{}, errors.New("injected profile failure")
		}
		return original(ctx, path, data, mode)
	}
	if _, err := manager.Enable(context.Background(), plan.Digest()); err == nil {
		t.Fatal("partial publication succeeded")
	}
	manager.write = original
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{plan.input.ProfilePath, plan.input.CatalogPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("partial file remains: %s", path)
		}
	}
	if _, _, err := planner.ownership.Current(context.Background()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ownership was published: %v", err)
	}
}

func TestManagerRecoversPartialUpdateToOwnedBase(t *testing.T) {
	manager, planner := newTestManager(t)
	defer func() { planner.options.BaseURL = "http://127.0.0.1:8787/v1" }()
	firstPlan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Enable(context.Background(), firstPlan.Digest())
	if err != nil {
		t.Fatal(err)
	}
	planner.options.BaseURL = "http://127.0.0.1:8788/v1"
	secondCatalog := append(firstPlan.Catalog(), '\n')
	digest := sha256.Sum256(secondCatalog)
	planner.catalog = catalogSourceFunc(func(context.Context) (control.CodexCatalogResponse, error) {
		return control.CodexCatalogResponse{APIVersion: control.APIVersion, Generation: 8, Catalog: string(secondCatalog), SHA256: hex.EncodeToString(digest[:])}, nil
	})
	secondPlan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	original := manager.write
	calls := 0
	manager.write = func(ctx context.Context, path string, data []byte, mode os.FileMode) (atomicfile.Result, error) {
		calls++
		if calls == 3 {
			return atomicfile.Result{}, errors.New("injected profile failure")
		}
		return original(ctx, path, data, mode)
	}
	if _, err := manager.Enable(context.Background(), secondPlan.Digest()); err == nil {
		t.Fatal("partial update succeeded")
	}
	manager.write = original
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string][]byte{firstPlan.input.ProfilePath: firstPlan.Profile(), firstPlan.input.CatalogPath: firstPlan.Catalog()} {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(expected) {
			t.Fatalf("%s = %q, %v", path, actual, err)
		}
	}
	_, current, err := planner.ownership.Current(context.Background())
	if err != nil || current.ID != first.GenerationID {
		t.Fatalf("current=%s want=%s err=%v", current.ID, first.GenerationID, err)
	}
}

func TestConcurrentManagersOnlyOneAcceptsProof(t *testing.T) {
	firstManager, planner := newTestManager(t)
	store := planner.ownership.(*codexownership.Store)
	secondManager, err := NewManager(planner, store, func() time.Time { return time.Unix(11, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, manager := range []*Manager{firstManager, secondManager} {
		go func(manager *Manager) {
			<-start
			_, err := manager.Enable(context.Background(), plan.Digest())
			results <- err
		}(manager)
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d", successes)
	}
}

func TestManagerRecoverRejectsCorruptPendingWithoutMutation(t *testing.T) {
	manager, planner := newTestManager(t)
	path := manager.pendingPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Recover(context.Background()); err == nil {
		t.Fatal("corrupt pending accepted")
	}
	profile := filepath.Join(planner.options.CodexHome, "ant-router.config.toml")
	if _, err := os.Lstat(profile); !os.IsNotExist(err) {
		t.Fatal("recovery mutated profile")
	}
}

func TestManagerRejectsStaleProofWithoutReplacingForeignFile(t *testing.T) {
	manager, planner := newTestManager(t)
	plan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.input.ProfilePath, []byte("foreign=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enable(context.Background(), plan.Digest()); err == nil {
		t.Fatal("stale proof accepted")
	}
	data, err := os.ReadFile(plan.input.ProfilePath)
	if err != nil || string(data) != "foreign=true\n" {
		t.Fatalf("foreign file changed: %q %v", data, err)
	}
}

func TestManagerRestoresPriorGenerationAndDisables(t *testing.T) {
	manager, planner := newTestManager(t)
	firstPlan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Enable(context.Background(), firstPlan.Digest())
	if err != nil {
		t.Fatal(err)
	}
	secondCatalog := append(firstPlan.Catalog(), '\n')
	digest := sha256.Sum256(secondCatalog)
	planner.catalog = catalogSourceFunc(func(context.Context) (control.CodexCatalogResponse, error) {
		return control.CodexCatalogResponse{APIVersion: control.APIVersion, Generation: 8, Catalog: string(secondCatalog), SHA256: hex.EncodeToString(digest[:])}, nil
	})
	secondPlan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Enable(context.Background(), secondPlan.Digest())
	if err != nil || second.GenerationID == first.GenerationID {
		t.Fatalf("second = %#v, %v", second, err)
	}
	restorePlan, err := planner.PlanRestore(context.Background(), first.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := manager.Restore(context.Background(), first.GenerationID, restorePlan.Digest())
	if err != nil || restored.GenerationID != first.GenerationID {
		t.Fatalf("restored = %#v, %v", restored, err)
	}
	for path, expected := range map[string][]byte{firstPlan.input.ProfilePath: firstPlan.Profile(), firstPlan.input.CatalogPath: firstPlan.Catalog()} {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(expected) {
			t.Fatalf("%s = %q, %v", path, actual, err)
		}
	}
	disablePlan, err := planner.PlanDisable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Disable(context.Background(), disablePlan.Digest()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{firstPlan.input.ProfilePath, firstPlan.input.CatalogPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("file remains: %s", path)
		}
	}
}
