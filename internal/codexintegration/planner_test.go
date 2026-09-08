package codexintegration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/codexcatalog"
	"github.com/abyssbugg/ant-router/internal/codexownership"
	"github.com/abyssbugg/ant-router/internal/codexversion"
	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/control"
)

type catalogSourceFunc func(context.Context) (control.CodexCatalogResponse, error)

func (f catalogSourceFunc) CodexCatalog(ctx context.Context) (control.CodexCatalogResponse, error) {
	return f(ctx)
}

type versionSourceFunc func(context.Context, bool) (codexversion.Version, error)

func (f versionSourceFunc) Version(ctx context.Context, allow bool) (codexversion.Version, error) {
	return f(ctx, allow)
}

func newTestPlanner(t *testing.T) (*Planner, PlannerOptions) {
	t.Helper()
	stateRoot, codexHome := t.TempDir(), t.TempDir()
	if err := os.Chmod(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := codexownership.NewStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := codexcatalog.Generate([]codexcatalog.Projection{{Alias: "mock/text", Capabilities: []string{"text"}, Metadata: config.CodexModelConfig{Enabled: true, DisplayName: "Mock Text", Description: "Deterministic text model", ContextWindow: 32768, EffectiveContextWindowPercent: 90, Priority: 10, Visibility: config.CodexVisibilityList}}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(catalog)
	options := PlannerOptions{CodexHome: codexHome, StateRoot: stateRoot, RouterctlPath: filepath.Join(stateRoot, "routerctl"), BaseURL: "http://127.0.0.1:8787/v1", Instance: "default"}
	planner, err := NewPlanner(options, catalogSourceFunc(func(context.Context) (control.CodexCatalogResponse, error) {
		return control.CodexCatalogResponse{APIVersion: control.APIVersion, Generation: 7, Catalog: string(catalog), SHA256: hex.EncodeToString(digest[:])}, nil
	}), versionSourceFunc(func(context.Context, bool) (codexversion.Version, error) {
		return codexversion.Version{Major: 0, Minor: codexversion.TestedMinor, Patch: 0}, nil
	}), store)
	if err != nil {
		t.Fatal(err)
	}
	return planner, options
}

func TestPlannerEnableIsReadOnlyAndProofBound(t *testing.T) {
	planner, options := newTestPlanner(t)
	plan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action() != ActionEnable || len(plan.Changes()) != 2 || len(plan.Profile()) == 0 || len(plan.Catalog()) == 0 {
		t.Fatalf("plan = %#v", plan)
	}
	for _, path := range []string{filepath.Join(options.CodexHome, "ant-router.config.toml"), filepath.Join(options.StateRoot, "codex", "ant-router", "models.json")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("planning created %s", path)
		}
	}
}

func publishPlan(t *testing.T, planner *Planner, plan Plan, createdAt time.Time) codexownership.Generation {
	t.Helper()
	store := planner.ownership.(*codexownership.Store)
	input := plan.input
	generation, err := store.Put(context.Background(), codexownership.Input{ProfilePath: input.ProfilePath, CatalogPath: input.CatalogPath, Profile: input.DesiredProfile, Catalog: input.DesiredCatalog, CodexVersion: input.CodexVersion, RouterctlPath: input.RouterctlPath, CreatedAt: createdAt})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(input.CatalogPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.CatalogPath, input.DesiredCatalog, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.ProfilePath, input.DesiredProfile, 0o600); err != nil {
		t.Fatal(err)
	}
	return generation
}

func TestPlannerOwnedEnableIsIdempotentAndRejectsDrift(t *testing.T) {
	planner, _ := newTestPlanner(t)
	initial, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publishPlan(t, planner, initial, time.Unix(1, 0).UTC())
	unchanged, err := planner.PlanEnable(context.Background())
	if err != nil || len(unchanged.Changes()) != 0 {
		t.Fatalf("unchanged = %#v, %v", unchanged.Changes(), err)
	}
	if err := os.WriteFile(initial.input.ProfilePath, []byte("drifted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := planner.PlanEnable(context.Background()); err == nil {
		t.Fatal("owned profile drift accepted")
	}
}

func TestPlannerDisableRemovesProfileBeforeCatalog(t *testing.T) {
	planner, _ := newTestPlanner(t)
	enable, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publishPlan(t, planner, enable, time.Unix(1, 0).UTC())
	plan, err := planner.PlanDisable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changes := plan.Changes()
	if len(changes) != 2 || changes[0].Path != enable.input.ProfilePath || changes[0].Kind != ChangeRemove || changes[1].Path != enable.input.CatalogPath || changes[1].Kind != ChangeRemove {
		t.Fatalf("changes = %#v", changes)
	}
	if _, err := os.Stat(enable.input.ProfilePath); err != nil {
		t.Fatal("planning removed profile")
	}
}

func TestPlannerRestorePublishesCatalogBeforeProfile(t *testing.T) {
	planner, _ := newTestPlanner(t)
	firstPlan, err := planner.PlanEnable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first := publishPlan(t, planner, firstPlan, time.Unix(1, 0).UTC())
	updatedInput := firstPlan.input
	updatedInput.DesiredProfile = append(append([]byte(nil), firstPlan.input.DesiredProfile...), []byte("# v2\n")...)
	updatedInput.DesiredCatalog = append(append([]byte(nil), firstPlan.input.DesiredCatalog...), []byte(" ")...)
	secondPlan, err := NewPlan(updatedInput)
	if err != nil {
		t.Fatal(err)
	}
	publishPlan(t, planner, secondPlan, time.Unix(2, 0).UTC())
	restore, err := planner.PlanRestore(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	changes := restore.Changes()
	if len(changes) != 2 || changes[0].Path != first.CatalogPath || changes[1].Path != first.ProfilePath {
		t.Fatalf("changes = %#v", changes)
	}
	if restore.input.OwnershipGeneration == first.ID {
		t.Fatal("restore proof did not bind current ownership generation")
	}
}

func TestPlannerRejectsUnownedProfileCollision(t *testing.T) {
	planner, options := newTestPlanner(t)
	path := filepath.Join(options.CodexHome, "ant-router.config.toml")
	if err := os.WriteFile(path, []byte("foreign=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := planner.PlanEnable(context.Background()); err == nil {
		t.Fatal("unowned profile collision accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "foreign=true\n" {
		t.Fatalf("foreign profile changed: %q %v", data, err)
	}
}
