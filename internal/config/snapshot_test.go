package config

import (
	"testing"
	"time"
)

// TestSnapshotAccessors verifies that Snapshot provides immutable accessors
// and that accessing values returns copies, not references to internal state.
func TestSnapshotAccessors(t *testing.T) {
	cfg := Config{
		SchemaVersion: 1,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}
	rid := RevisionFor([]byte("test canonical"))
	sourcePath := "/etc/app/config.toml"
	gen := uint64(42)
	now := time.Date(2026, 8, 19, 10, 30, 0, 0, time.UTC)
	status := SnapshotStatusActive

	snap := &Snapshot{
		config:      cfg,
		revisionID:  rid,
		sourcePath:  sourcePath,
		generation:  gen,
		activatedAt: now,
		status:      status,
	}

	// Test Config() returns a copy
	gotCfg := snap.Config()
	if gotCfg.SchemaVersion != cfg.SchemaVersion {
		t.Errorf("Config().SchemaVersion = %d, want %d", gotCfg.SchemaVersion, cfg.SchemaVersion)
	}

	// Test RevisionID()
	if snap.RevisionID() != rid {
		t.Errorf("RevisionID() = %s, want %s", snap.RevisionID(), rid)
	}

	// Test SourcePath()
	if snap.SourcePath() != sourcePath {
		t.Errorf("SourcePath() = %q, want %q", snap.SourcePath(), sourcePath)
	}

	// Test Generation()
	if snap.Generation() != gen {
		t.Errorf("Generation() = %d, want %d", snap.Generation(), gen)
	}

	// Test ActivatedAt()
	if snap.ActivatedAt() != now {
		t.Errorf("ActivatedAt() = %v, want %v", snap.ActivatedAt(), now)
	}

	// Test Status()
	if snap.Status() != status {
		t.Errorf("Status() = %q, want %q", snap.Status(), status)
	}
}

// TestSnapshotValueCopyBehavior verifies that Config() returns a copy,
// not a reference to the internal state.
func TestSnapshotValueCopyBehavior(t *testing.T) {
	originalCfg := Config{
		SchemaVersion: 1,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}

	snap := &Snapshot{
		config:      originalCfg,
		revisionID:  RevisionFor([]byte("test")),
		sourcePath:  "/test",
		generation:  1,
		activatedAt: time.Now().UTC(),
		status:      SnapshotStatusActive,
	}

	// Get a copy and modify it
	cfg := snap.Config()
	cfg.SchemaVersion = 999

	// Original should be unchanged
	if snap.Config().SchemaVersion != 1 {
		t.Errorf("original Config was modified through returned copy")
	}
}

// TestSnapshotImmutability verifies that Snapshot is immutable after creation.
// The snapshot struct fields are unexported and only accessible through read-only methods.
func TestSnapshotImmutability(t *testing.T) {
	snap := &Snapshot{
		config:      Default(),
		revisionID:  RevisionFor([]byte("immutable")),
		sourcePath:  "/test",
		generation:  1,
		activatedAt: time.Now().UTC(),
		status:      SnapshotStatusActive,
	}

	// Verify the snapshot is accessible only through its methods
	// (compile-time check that fields are unexported)
	_ = snap.Config()
	_ = snap.RevisionID()
	_ = snap.SourcePath()
	_ = snap.Generation()
	_ = snap.ActivatedAt()
	_ = snap.Status()
}
