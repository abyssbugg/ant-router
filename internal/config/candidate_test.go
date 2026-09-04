package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNewCandidateBasic(t *testing.T) {
	cfg := Default()
	testPath := filepath.Join(t.TempDir(), "test.toml")
	cand, err := NewCandidate(cfg, testPath, 42)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	if !reflect.DeepEqual(cand.Config(), cfg) {
		t.Error("Config() mismatch")
	}

	if cand.SourcePath() != testPath {
		t.Errorf("SourcePath() = %q, want %q", cand.SourcePath(), testPath)
	}

	if cand.BaseGeneration() != 42 {
		t.Errorf("BaseGeneration() = %d, want 42", cand.BaseGeneration())
	}
}

func TestNewCandidateValidatesConfig(t *testing.T) {
	invalidCfg := Config{
		SchemaVersion: 0, // Invalid
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}

	_, err := NewCandidate(invalidCfg, "", 0)
	if err == nil {
		t.Error("NewCandidate should reject invalid config")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeUnsupportedSchema {
		t.Errorf("Expected CodeUnsupportedSchema, got %v", code)
	}
}

func TestNewCandidateBlankSourcePath(t *testing.T) {
	cfg := Default()
	cand, err := NewCandidate(cfg, "", 0)
	if err != nil {
		t.Fatalf("NewCandidate with blank source failed: %v", err)
	}

	if cand.SourcePath() != "" {
		t.Errorf("SourcePath() = %q, want empty string for blank source", cand.SourcePath())
	}
}

func TestNewCandidateRelativePathBecomesAbsolute(t *testing.T) {
	cfg := Default()
	cand, err := NewCandidate(cfg, "test.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate with relative path failed: %v", err)
	}

	sourcePath := cand.SourcePath()
	if !filepath.IsAbs(sourcePath) {
		t.Errorf("SourcePath() = %q, want absolute path", sourcePath)
	}

	// Verify it ends with test.toml
	if !bytes.Contains([]byte(sourcePath), []byte("test.toml")) {
		t.Errorf("SourcePath() = %q, want to contain 'test.toml'", sourcePath)
	}
}

func TestNewCandidateCleanedPath(t *testing.T) {
	cfg := Default()
	cand, err := NewCandidate(cfg, "./test/./config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate with dirty path failed: %v", err)
	}

	sourcePath := cand.SourcePath()
	if filepath.Clean(sourcePath) != sourcePath {
		t.Errorf("SourcePath() not cleaned: %q", sourcePath)
	}

	// Should not contain ./ or redundant elements
	if bytes.Contains([]byte(sourcePath), []byte("./")) {
		t.Errorf("SourcePath() contains ./: %q", sourcePath)
	}
}

func TestNewCandidateGeneratesCanonical(t *testing.T) {
	cfg := Default()
	cand, err := NewCandidate(cfg, "", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	canonical := cand.Canonical()
	if len(canonical) == 0 {
		t.Error("Canonical() returned empty bytes")
	}

	// Verify it's valid TOML
	decoded, err := Decode(bytes.NewReader(canonical))
	if err != nil {
		t.Fatalf("Canonical output is not valid TOML: %v", err)
	}

	if decoded.SchemaVersion != cfg.SchemaVersion {
		t.Error("Canonical TOML semantically different")
	}
}

func TestNewCandidateGeneratesRevisionID(t *testing.T) {
	cfg := Default()
	testPath := filepath.Join(t.TempDir(), "test.toml")
	cand, err := NewCandidate(cfg, testPath, 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	rid := cand.RevisionID()
	if !rid.Valid() {
		t.Errorf("RevisionID.Valid() = false, want true")
	}

	// Verify it matches the canonical bytes
	expectedRid := RevisionFor(cand.Canonical())
	if rid != expectedRid {
		t.Errorf("RevisionID mismatch: got %s, want %s", rid, expectedRid)
	}
}

func TestCandidateCanonicalReturnsCopy(t *testing.T) {
	cfg := Default()
	cand, err := NewCandidate(cfg, "", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	canonical1 := cand.Canonical()
	canonical2 := cand.Canonical()

	// Should be equal but not same reference
	if !bytes.Equal(canonical1, canonical2) {
		t.Error("Canonical() returned different values on repeated calls")
	}

	// Mutate one and verify the other and candidate are unaffected
	if len(canonical1) > 0 {
		canonical1[0] = canonical1[0] ^ 0xFF
	}

	canonical3 := cand.Canonical()
	if bytes.Equal(canonical1, canonical3) {
		t.Error("Mutating returned Canonical() affected candidate's internal state")
	}
}

func TestLoadCandidateBasic(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.toml")

	configContent := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cand, err := LoadCandidate(configPath, 99)
	if err != nil {
		t.Fatalf("LoadCandidate failed: %v", err)
	}

	if cand.SourcePath() != configPath {
		t.Errorf("SourcePath() = %q, want %q", cand.SourcePath(), configPath)
	}

	if cand.BaseGeneration() != 99 {
		t.Errorf("BaseGeneration() = %d, want 99", cand.BaseGeneration())
	}

	// Verify config is loaded correctly
	cfg := cand.Config()
	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want 4", cfg.SchemaVersion)
	}
}

func TestLoadCandidateFileNotFound(t *testing.T) {
	_, err := LoadCandidate("/nonexistent/path/config.toml", 0)
	if err == nil {
		t.Error("LoadCandidate should return error for nonexistent file")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeSourceNotFound {
		t.Errorf("Expected CodeSourceNotFound, got %v", code)
	}
}

func TestLoadCandidateInvalidTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.toml")

	invalidContent := `schema_version = 1
this is not valid toml
`

	if err := os.WriteFile(configPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := LoadCandidate(configPath, 0)
	if err == nil {
		t.Error("LoadCandidate should return error for invalid TOML")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeDecodeFailed {
		t.Errorf("Expected CodeDecodeFailed, got %v", code)
	}
}

func TestLoadCandidateInvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_config.toml")

	invalidContent := `schema_version = 0
[daemon]
shutdown_timeout = "30s"
`

	if err := os.WriteFile(configPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := LoadCandidate(configPath, 0)
	if err == nil {
		t.Error("LoadCandidate should return error for invalid config")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeUnsupportedSchema {
		t.Errorf("Expected CodeUnsupportedSchema, got %v", code)
	}
}

func TestLoadCandidateCapturesBaseGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.toml")

	configContent := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	for _, gen := range []uint64{0, 1, 42, 999999} {
		cand, err := LoadCandidate(configPath, gen)
		if err != nil {
			t.Fatalf("LoadCandidate failed: %v", err)
		}

		if cand.BaseGeneration() != gen {
			t.Errorf("BaseGeneration() = %d, want %d", cand.BaseGeneration(), gen)
		}
	}
}

func TestCandidateAccessors(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{45 * time.Second},
		},
	}

	testPath := filepath.Join(t.TempDir(), "config.toml")
	cand, err := NewCandidate(cfg, testPath, 123)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	// Test Config()
	gotCfg := cand.Config()
	if gotCfg.SchemaVersion != cfg.SchemaVersion {
		t.Errorf("Config().SchemaVersion = %d, want %d", gotCfg.SchemaVersion, cfg.SchemaVersion)
	}

	// Test Canonical()
	canonical := cand.Canonical()
	if len(canonical) == 0 {
		t.Error("Canonical() returned empty")
	}

	// Test RevisionID()
	rid := cand.RevisionID()
	if !rid.Valid() {
		t.Errorf("RevisionID().Valid() = false, want true")
	}

	// Test SourcePath()
	if cand.SourcePath() != testPath {
		t.Errorf("SourcePath() = %q, want %q", cand.SourcePath(), testPath)
	}

	// Test BaseGeneration()
	if cand.BaseGeneration() != 123 {
		t.Errorf("BaseGeneration() = %d, want 123", cand.BaseGeneration())
	}
}

func TestCandidateDifferentConfigsDifferentRevisions(t *testing.T) {
	cfg1 := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}

	cfg2 := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{60 * time.Second},
		},
	}

	cand1, err := NewCandidate(cfg1, "/path/to/config1.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate(cfg1) failed: %v", err)
	}

	cand2, err := NewCandidate(cfg2, "/path/to/config2.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate(cfg2) failed: %v", err)
	}

	if cand1.RevisionID() == cand2.RevisionID() {
		t.Error("Different configs should have different RevisionIDs")
	}
}

func TestCandidateSameConfigSameRevision(t *testing.T) {
	cfg := Default()

	cand1, err := NewCandidate(cfg, "/path/to/config1.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate(cfg) #1 failed: %v", err)
	}

	cand2, err := NewCandidate(cfg, "/path/to/config2.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate(cfg) #2 failed: %v", err)
	}

	// Same config should have same RevisionID regardless of source path
	if cand1.RevisionID() != cand2.RevisionID() {
		t.Error("Same config should have same RevisionID")
	}
}

func TestLoadCandidatePreservesSourcePath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.toml")

	configContent := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cand, err := LoadCandidate(configPath, 0)
	if err != nil {
		t.Fatalf("LoadCandidate failed: %v", err)
	}

	// SourcePath should match exactly what was passed
	if cand.SourcePath() != configPath {
		t.Errorf("SourcePath() = %q, want %q", cand.SourcePath(), configPath)
	}
}

func TestLoadCandidateRelativePathBecomesAbsolute(t *testing.T) {
	// Create the file on the working-directory volume so the relative path is portable.
	tmpDir, err := os.MkdirTemp(".", "tmp-candidate-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	configPath := filepath.Join(tmpDir, "test.toml")

	configContent := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cand, err := LoadCandidate(configPath, 0)
	if err != nil {
		t.Fatalf("LoadCandidate with relative path failed: %v", err)
	}

	sourcePath := cand.SourcePath()
	if !filepath.IsAbs(sourcePath) {
		t.Errorf("SourcePath() = %q, want absolute path", sourcePath)
	}

	expectedPath, err := filepath.Abs(configPath)
	if err != nil {
		t.Fatalf("filepath.Abs() failed: %v", err)
	}
	if filepath.Clean(sourcePath) != filepath.Clean(expectedPath) {
		t.Errorf("SourcePath() = %q, want %q", filepath.Clean(sourcePath), filepath.Clean(expectedPath))
	}
}

func TestNewCandidateRelativeSourcePathResolvedToAbsolute(t *testing.T) {
	cfg := Default()
	// Test that relative paths get resolved to absolute paths.
	// filepath.Abs resolution is guarded by the helper function.
	// If resolution fails, NewCandidate returns CodePathResolutionFailed error
	// (verified by unit test of resolveAbsolutePath).

	cand, err := NewCandidate(cfg, "relative/path.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate with relative path failed: %v", err)
	}

	sourcePath := cand.SourcePath()
	if !filepath.IsAbs(sourcePath) {
		t.Errorf("SourcePath() = %q, want absolute path for nonblank source", sourcePath)
	}
}
