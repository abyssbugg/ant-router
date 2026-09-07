package codexintegration

import (
	"path/filepath"
	"testing"
)

func validPlanInput(t *testing.T) PlanInput {
	t.Helper()
	root := t.TempDir()
	return PlanInput{Action: ActionEnable, ProfilePath: filepath.Join(root, "ant-router.config.toml"), CatalogPath: filepath.Join(root, "models.json"), RouterctlPath: filepath.Join(root, "routerctl"), CodexVersion: "codex-cli 0.92.0", ConfigGeneration: 7, CurrentProfile: FileObservation{}, CurrentCatalog: FileObservation{}, OwnershipGeneration: "", DesiredProfile: []byte("profile-v1"), DesiredCatalog: []byte("catalog-v1")}
}

func TestPlanIsDeterministicImmutableAndOrdered(t *testing.T) {
	input := validPlanInput(t)
	first, err := NewPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() || len(first.Digest()) != 64 {
		t.Fatalf("digests = %q %q", first.Digest(), second.Digest())
	}
	changes := first.Changes()
	if len(changes) != 2 || changes[0].Path != input.CatalogPath || changes[1].Path != input.ProfilePath || changes[0].Kind != ChangeCreate || changes[1].Kind != ChangeCreate {
		t.Fatalf("changes = %#v", changes)
	}
	profile := first.Profile()
	catalog := first.Catalog()
	profile[0], catalog[0], changes[0].Path = 'X', 'X', "changed"
	if string(first.Profile()) != "profile-v1" || string(first.Catalog()) != "catalog-v1" || first.Changes()[0].Path != input.CatalogPath {
		t.Fatal("plan exposed mutable state")
	}
}

func TestPlanDigestBindsEveryExternalObservation(t *testing.T) {
	base := validPlanInput(t)
	original, err := NewPlan(base)
	if err != nil {
		t.Fatal(err)
	}
	variants := []PlanInput{base, base, base, base, base, base}
	variants[0].ConfigGeneration++
	variants[1].CodexVersion = "codex-cli 0.93.0"
	variants[2].CurrentProfile = FileObservation{Exists: true, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	variants[3].CurrentCatalog = FileObservation{Exists: true, SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	variants[4].OwnershipGeneration = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	variants[5].DesiredCatalog = []byte("catalog-v2")
	for index, variant := range variants {
		plan, err := NewPlan(variant)
		if err != nil {
			t.Fatalf("variant %d: %v", index, err)
		}
		if plan.Digest() == original.Digest() {
			t.Errorf("variant %d did not change digest", index)
		}
	}
}

func TestPlanRejectsInvalidInputs(t *testing.T) {
	valid := validPlanInput(t)
	cases := []PlanInput{valid, valid, valid, valid}
	cases[0].Action = "unknown"
	cases[1].ProfilePath = "relative"
	cases[2].ConfigGeneration = 0
	cases[3].DesiredCatalog = nil
	for index, input := range cases {
		if _, err := NewPlan(input); err == nil {
			t.Errorf("case %d accepted", index)
		}
	}
}
