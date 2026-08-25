package configservice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/config"
)

func TestApplyRepairsIdenticalCorruptRevision(t *testing.T) {
	root := t.TempDir()
	seed, _ := newService(t, root)
	_, _ = seed.Initialize(context.Background())
	data := canonicalConfig(t, 30*time.Second)
	validation, _ := seed.Validate(context.Background(), data)
	active, err := seed.Apply(context.Background(), ApplyRequest{TOML: data, ExpectedRevision: validation.Revision, ExpectedGeneration: 0})
	if err != nil {
		t.Fatalf("seed Apply() failed: %v", err)
	}
	path := filepath.Join(root, "revisions", active.Revision.String()+".toml")
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(corrupt) failed: %v", err)
	}

	degraded, _ := newService(t, root)
	state, err := degraded.Initialize(context.Background())
	if err == nil || state != StateDegraded {
		t.Fatalf("degraded Initialize() = %q, %v", state, err)
	}
	proof, err := degraded.Validate(context.Background(), data)
	if err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	repaired, err := degraded.Apply(context.Background(), ApplyRequest{TOML: data, ExpectedRevision: proof.Revision, ExpectedGeneration: proof.BaseGeneration})
	if err != nil {
		t.Fatalf("repair Apply() failed: %v", err)
	}
	if repaired.State != StateRunning || repaired.Revision != config.RevisionID(active.Revision) {
		t.Errorf("repaired = %#v", repaired)
	}
}
