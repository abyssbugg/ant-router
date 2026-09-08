package codexintegration

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
)

func FuzzNewPlan(f *testing.F) {
	f.Add(byte(0), []byte("profile"), []byte("catalog"), false, false)
	root, err := filepath.Abs("tmp_rovodev_plan_fuzz")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, selector byte, profile, catalog []byte, profileExists, catalogExists bool) {
		if len(profile) > 4096 || len(catalog) > 4096 {
			return
		}
		action := []Action{ActionEnable, ActionDisable, ActionRestore}[int(selector)%3]
		input := PlanInput{Action: action, ProfilePath: filepath.Join(root, "profile"), CatalogPath: filepath.Join(root, "catalog"), RouterctlPath: filepath.Join(root, "routerctl"), CodexVersion: "0.153.0", ConfigGeneration: 1, OwnershipGeneration: stringsRepeat("a", 64), DesiredProfile: append([]byte(nil), profile...), DesiredCatalog: append([]byte(nil), catalog...)}
		if action == ActionDisable {
			input.DesiredProfile, input.DesiredCatalog = nil, nil
		}
		if profileExists {
			digest := sha256.Sum256([]byte("observed-profile"))
			input.CurrentProfile = FileObservation{Exists: true, SHA256: hex.EncodeToString(digest[:])}
		}
		if catalogExists {
			digest := sha256.Sum256([]byte("observed-catalog"))
			input.CurrentCatalog = FileObservation{Exists: true, SHA256: hex.EncodeToString(digest[:])}
		}
		first, err := NewPlan(input)
		if err != nil {
			return
		}
		second, err := NewPlan(input)
		if err != nil || first.Digest() != second.Digest() || !validDigest(first.Digest()) {
			t.Fatal("plan proof was not deterministic")
		}
	})
}

func stringsRepeat(value string, count int) string {
	result := make([]byte, 0, len(value)*count)
	for range count {
		result = append(result, value...)
	}
	return string(result)
}
