package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestRevisionIDString(t *testing.T) {
	rid := RevisionID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	got := rid.String()
	want := "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	if got != want {
		t.Errorf("RevisionID.String() = %q, want %q", got, want)
	}
}

func TestRevisionIDValid(t *testing.T) {
	tests := []struct {
		name string
		rid  RevisionID
		want bool
	}{
		{
			name: "valid lowercase hex 64 chars",
			rid:  RevisionID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd"),
			want: true,
		},
		{
			name: "uppercase rejects",
			rid:  RevisionID("ABC123DEF456ABC123DEF456ABC123DEF456ABC123DEF456ABC123DEF456ABCD"),
			want: false,
		},
		{
			name: "mixed case rejects",
			rid:  RevisionID("abc123DEF456abc123def456abc123def456abc123def456abc123def456abcd"),
			want: false,
		},
		{
			name: "too short",
			rid:  RevisionID("abc123def456abc123def456abc123def456abc123def456abc123def456abc"),
			want: false,
		},
		{
			name: "too long",
			rid:  RevisionID("abc123def456abc123def456abc123def456abc123def456abc123def456abcde"),
			want: false,
		},
		{
			name: "non-hex character",
			rid:  RevisionID("xyz123def456abc123def456abc123def456abc123def456abc123def456abcd"),
			want: false,
		},
		{
			name: "empty",
			rid:  RevisionID(""),
			want: false,
		},
		{
			name: "all zeros valid",
			rid:  RevisionID("0000000000000000000000000000000000000000000000000000000000000000"),
			want: true,
		},
		{
			name: "all f valid",
			rid:  RevisionID("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rid.Valid()
			if got != tt.want {
				t.Errorf("RevisionID.Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanonical(t *testing.T) {
	cfg := Default()
	data, err := Canonical(cfg)
	if err != nil {
		t.Fatalf("Canonical(Default()) failed: %v", err)
	}
	if len(data) == 0 {
		t.Error("Canonical returned empty bytes")
	}

	// Verify it's valid TOML
	decoded, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Canonical output is not valid TOML: %v", err)
	}

	// Verify roundtrip semantic equivalence
	if decoded.SchemaVersion != cfg.SchemaVersion {
		t.Errorf("Roundtrip schema version mismatch: %d != %d", decoded.SchemaVersion, cfg.SchemaVersion)
	}
}

func TestCanonicalInvalidConfig(t *testing.T) {
	invalidCfg := Config{
		SchemaVersion: 0, // Invalid
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}

	_, err := Canonical(invalidCfg)
	if err == nil {
		t.Error("Canonical(invalid config) should return error")
	}

	// Verify it's a validation error
	code, ok := CodeOf(err)
	if !ok || code != CodeUnsupportedSchema {
		t.Errorf("Expected CodeUnsupportedSchema, got %v", code)
	}
}

func TestCanonicalDeterministic(t *testing.T) {
	cfg := Default()

	// Encode same config multiple times
	data1, err1 := Canonical(cfg)
	data2, err2 := Canonical(cfg)
	data3, err3 := Canonical(cfg)

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("Canonical() errors: %v, %v, %v", err1, err2, err3)
	}

	if !bytes.Equal(data1, data2) {
		t.Error("Canonical output not deterministic: first != second")
	}
	if !bytes.Equal(data2, data3) {
		t.Error("Canonical output not deterministic: second != third")
	}
}

func TestCanonicalIgnoresComments(t *testing.T) {
	// Two configs with same semantic content but comments should produce same canonical
	toml1 := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
`

	toml2 := `# This is a comment
schema_version = 1

# Another comment
[daemon]
# Daemon config
shutdown_timeout = "30s"
# End config
`

	cfg1, err := Decode(bytes.NewReader([]byte(toml1)))
	if err != nil {
		t.Fatalf("Failed to decode toml1: %v", err)
	}

	cfg2, err := Decode(bytes.NewReader([]byte(toml2)))
	if err != nil {
		t.Fatalf("Failed to decode toml2: %v", err)
	}

	can1, err := Canonical(cfg1)
	if err != nil {
		t.Fatalf("Canonical(cfg1) failed: %v", err)
	}

	can2, err := Canonical(cfg2)
	if err != nil {
		t.Fatalf("Canonical(cfg2) failed: %v", err)
	}

	if !bytes.Equal(can1, can2) {
		t.Error("Canonical output differs for semantically equivalent configs with different comments")
	}
}

func TestCanonicalIgnoresWhitespace(t *testing.T) {
	// Two configs with different whitespace should produce same canonical
	toml1 := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
`

	toml2 := `schema_version=1
[daemon]
shutdown_timeout="30s"
`

	cfg1, err := Decode(bytes.NewReader([]byte(toml1)))
	if err != nil {
		t.Fatalf("Failed to decode toml1: %v", err)
	}

	cfg2, err := Decode(bytes.NewReader([]byte(toml2)))
	if err != nil {
		t.Fatalf("Failed to decode toml2: %v", err)
	}

	can1, err := Canonical(cfg1)
	if err != nil {
		t.Fatalf("Canonical(cfg1) failed: %v", err)
	}

	can2, err := Canonical(cfg2)
	if err != nil {
		t.Fatalf("Canonical(cfg2) failed: %v", err)
	}

	if !bytes.Equal(can1, can2) {
		t.Error("Canonical output differs for semantically equivalent configs with different whitespace")
	}
}

func TestCanonicalChangedTimeoutChangesRevision(t *testing.T) {
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

	can1, err := Canonical(cfg1)
	if err != nil {
		t.Fatalf("Canonical(cfg1) failed: %v", err)
	}

	can2, err := Canonical(cfg2)
	if err != nil {
		t.Fatalf("Canonical(cfg2) failed: %v", err)
	}

	if bytes.Equal(can1, can2) {
		t.Error("Canonical output should differ for different timeout values")
	}

	rid1 := RevisionFor(can1)
	rid2 := RevisionFor(can2)

	if rid1 == rid2 {
		t.Errorf("RevisionID should differ: %s == %s", rid1, rid2)
	}
}

func TestRevisionFor(t *testing.T) {
	data := []byte("test data")
	rid := RevisionFor(data)

	// Verify format: 64 lowercase hex characters
	if len(rid.String()) != 64 {
		t.Errorf("RevisionID length = %d, want 74", len(rid.String()))
	}

	// Verify it's valid
	if !rid.Valid() {
		t.Errorf("RevisionID.Valid() = false, want true")
	}

	// Verify it matches SHA-256
	hash := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(hash[:])
	if rid.String() != expectedHex {
		t.Errorf("RevisionID mismatch: got %s, want %s", rid.String(), expectedHex)
	}
}

func TestRevisionForDeterministic(t *testing.T) {
	data := []byte("test data")

	rid1 := RevisionFor(data)
	rid2 := RevisionFor(data)

	if rid1 != rid2 {
		t.Errorf("RevisionFor not deterministic: %s != %s", rid1, rid2)
	}
}

func TestRevisionForDifferentData(t *testing.T) {
	data1 := []byte("test data 1")
	data2 := []byte("test data 2")

	rid1 := RevisionFor(data1)
	rid2 := RevisionFor(data2)

	if rid1 == rid2 {
		t.Errorf("RevisionFor should differ for different data: %s == %s", rid1, rid2)
	}
}

func TestDecodeAndCanonicalSemanticEquality(t *testing.T) {
	originalTOML := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
`

	// Decode original
	cfg1, err := Decode(bytes.NewReader([]byte(originalTOML)))
	if err != nil {
		t.Fatalf("Failed to decode original TOML: %v", err)
	}

	// Get canonical
	canonical, err := Canonical(cfg1)
	if err != nil {
		t.Fatalf("Canonical failed: %v", err)
	}

	// Decode canonical
	cfg2, err := Decode(bytes.NewReader(canonical))
	if err != nil {
		t.Fatalf("Failed to decode canonical: %v", err)
	}

	// Verify semantic equivalence
	if cfg1.SchemaVersion != cfg2.SchemaVersion {
		t.Errorf("SchemaVersion mismatch: %d != %d", cfg1.SchemaVersion, cfg2.SchemaVersion)
	}

	if cfg1.Daemon.ShutdownTimeout != cfg2.Daemon.ShutdownTimeout {
		t.Errorf("ShutdownTimeout mismatch: %v != %v",
			cfg1.Daemon.ShutdownTimeout,
			cfg2.Daemon.ShutdownTimeout)
	}
}
