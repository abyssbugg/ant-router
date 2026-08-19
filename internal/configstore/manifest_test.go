package configstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

func TestManifestValidateSuccess(t *testing.T) {
	now := time.Now()
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	if err := manifest.Validate(); err != nil {
		t.Errorf("Validate() failed unexpectedly: %v", err)
	}
}

func TestManifestValidateWithPrevious(t *testing.T) {
	now := time.Now()
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    2,
		Current:       revision.For([]byte("current")),
		Previous:      revision.For([]byte("previous")),
		ActivatedAt:   now,
	}

	if err := manifest.Validate(); err != nil {
		t.Errorf("Validate() failed unexpectedly: %v", err)
	}
}

func TestManifestValidateInvalidFormat(t *testing.T) {
	now := time.Now()
	manifest := Manifest{
		FormatVersion: 2, // Wrong version
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() should have failed for invalid format version")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("Validate() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestManifestValidateZeroGeneration(t *testing.T) {
	now := time.Now()
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    0, // Invalid
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() should have failed for zero generation")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("Validate() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestManifestValidateInvalidCurrent(t *testing.T) {
	now := time.Now()
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.ID("invalid"), // Invalid revision
		Previous:      "",
		ActivatedAt:   now,
	}

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() should have failed for invalid current revision")
	}

	code, found := CodeOf(err)
	if !found || code != CodeInvalidRevision {
		t.Errorf("Validate() returned code %q, want %q", code, CodeInvalidRevision)
	}
}

func TestManifestValidateInvalidPrevious(t *testing.T) {
	now := time.Now()
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    2,
		Current:       revision.For([]byte("current")),
		Previous:      revision.ID("invalid"), // Invalid revision
		ActivatedAt:   now,
	}

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() should have failed for invalid previous revision")
	}

	code, found := CodeOf(err)
	if !found || code != CodeInvalidRevision {
		t.Errorf("Validate() returned code %q, want %q", code, CodeInvalidRevision)
	}
}

func TestManifestValidateZeroActivatedAt(t *testing.T) {
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   time.Time{}, // Zero time
	}

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() should have failed for zero activated time")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("Validate() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestEncodeManifestSuccess(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest() failed: %v", err)
	}

	// Should be valid JSON
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Errorf("Encoded data is not valid JSON: %v", err)
	}

	// Should end with newline
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Errorf("EncodeManifest() should end with newline")
	}
}

func TestEncodeManifestDeterministic(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	data1, err1 := EncodeManifest(manifest)
	if err1 != nil {
		t.Fatalf("EncodeManifest() failed: %v", err1)
	}

	data2, err2 := EncodeManifest(manifest)
	if err2 != nil {
		t.Fatalf("EncodeManifest() failed: %v", err2)
	}

	if !bytes.Equal(data1, data2) {
		t.Errorf("EncodeManifest() not deterministic:\nFirst:  %s\nSecond: %s", data1, data2)
	}
}

func TestEncodeManifestInvalidFails(t *testing.T) {
	manifest := Manifest{
		FormatVersion: 2, // Invalid
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   time.Now(),
	}

	_, err := EncodeManifest(manifest)
	if err == nil {
		t.Fatal("EncodeManifest() should fail for invalid manifest")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("EncodeManifest() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestDecodeManifestSuccess(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	original := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	encoded, err := EncodeManifest(original)
	if err != nil {
		t.Fatalf("EncodeManifest() failed: %v", err)
	}

	decoded, err := DecodeManifest(encoded)
	if err != nil {
		t.Fatalf("DecodeManifest() failed: %v", err)
	}

	if decoded.FormatVersion != original.FormatVersion {
		t.Errorf("FormatVersion mismatch: got %d, want %d", decoded.FormatVersion, original.FormatVersion)
	}
	if decoded.Generation != original.Generation {
		t.Errorf("Generation mismatch: got %d, want %d", decoded.Generation, original.Generation)
	}
	if decoded.Current != original.Current {
		t.Errorf("Current mismatch: got %q, want %q", decoded.Current, original.Current)
	}
	if decoded.Previous != original.Previous {
		t.Errorf("Previous mismatch: got %q, want %q", decoded.Previous, original.Previous)
	}
	if !decoded.ActivatedAt.Equal(original.ActivatedAt) {
		t.Errorf("ActivatedAt mismatch: got %v, want %v", decoded.ActivatedAt, original.ActivatedAt)
	}
}

func TestDecodeManifestEmptyFails(t *testing.T) {
	_, err := DecodeManifest([]byte(""))
	if err == nil {
		t.Fatal("DecodeManifest() should reject empty input")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("DecodeManifest() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestDecodeManifestMalformedJSON(t *testing.T) {
	_, err := DecodeManifest([]byte("{invalid json"))
	if err == nil {
		t.Fatal("DecodeManifest() should reject malformed JSON")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("DecodeManifest() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestDecodeManifestUnknownFields(t *testing.T) {
	data := []byte(`{
  "format_version": 1,
  "generation": 1,
  "current": "0000000000000000000000000000000000000000000000000000000000000000",
  "previous": "",
  "activated_at": "2025-01-01T12:00:00Z",
  "unknown_field": "should_fail"
}
`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("DecodeManifest() should reject unknown fields")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("DecodeManifest() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestDecodeManifestTrailingJSON(t *testing.T) {
	data := []byte(`{
  "format_version": 1,
  "generation": 1,
  "current": "0000000000000000000000000000000000000000000000000000000000000000",
  "previous": "",
  "activated_at": "2025-01-01T12:00:00Z"
}
{"extra": "json"}
`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("DecodeManifest() should reject trailing JSON")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("DecodeManifest() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestDecodeManifestInvalidID(t *testing.T) {
	data := []byte(`{
  "format_version": 1,
  "generation": 1,
  "current": "invalid_revision_id",
  "previous": "",
  "activated_at": "2025-01-01T12:00:00Z"
}
`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("DecodeManifest() should reject invalid revision ID")
	}

	code, found := CodeOf(err)
	if !found || code != CodeInvalidRevision {
		t.Errorf("DecodeManifest() returned code %q, want %q", code, CodeInvalidRevision)
	}
}

func TestDecodeManifestInvalidTime(t *testing.T) {
	data := []byte(`{
  "format_version": 1,
  "generation": 1,
  "current": "0000000000000000000000000000000000000000000000000000000000000000",
  "previous": "",
  "activated_at": "not-a-timestamp"
}
`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("DecodeManifest() should reject invalid timestamp")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("DecodeManifest() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestDecodeManifestInvalidGeneration(t *testing.T) {
	data := []byte(`{
  "format_version": 1,
  "generation": 0,
  "current": "0000000000000000000000000000000000000000000000000000000000000000",
  "previous": "",
  "activated_at": "2025-01-01T12:00:00Z"
}
`)

	_, err := DecodeManifest(data)
	if err == nil {
		t.Fatal("DecodeManifest() should reject zero generation")
	}

	code, found := CodeOf(err)
	if !found || code != CodeManifestCorrupt {
		t.Errorf("DecodeManifest() returned code %q, want %q", code, CodeManifestCorrupt)
	}
}

func TestManifestRoundtrip(t *testing.T) {
	now := time.Date(2025, 6, 15, 14, 30, 45, 0, time.UTC)
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    5,
		Current:       revision.For([]byte("current data")),
		Previous:      revision.For([]byte("previous data")),
		ActivatedAt:   now,
	}

	encoded, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest() failed: %v", err)
	}

	decoded, err := DecodeManifest(encoded)
	if err != nil {
		t.Fatalf("DecodeManifest() failed: %v", err)
	}

	// Compare all fields
	if decoded.FormatVersion != manifest.FormatVersion {
		t.Errorf("FormatVersion mismatch: got %d, want %d", decoded.FormatVersion, manifest.FormatVersion)
	}
	if decoded.Generation != manifest.Generation {
		t.Errorf("Generation mismatch: got %d, want %d", decoded.Generation, manifest.Generation)
	}
	if decoded.Current != manifest.Current {
		t.Errorf("Current mismatch: got %q, want %q", decoded.Current, manifest.Current)
	}
	if decoded.Previous != manifest.Previous {
		t.Errorf("Previous mismatch: got %q, want %q", decoded.Previous, manifest.Previous)
	}
	if !decoded.ActivatedAt.Equal(manifest.ActivatedAt) {
		t.Errorf("ActivatedAt mismatch: got %v, want %v", decoded.ActivatedAt, manifest.ActivatedAt)
	}
}

func TestManifestJSONTags(t *testing.T) {
	// Test that JSON tags are stable and correct
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest() failed: %v", err)
	}

	// Decode as generic JSON to check field names
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}

	expectedKeys := map[string]bool{
		"format_version": true,
		"generation":     true,
		"current":        true,
		"activated_at":   true,
	}

	if _, ok := m["previous"]; ok {
		t.Error("previous must be omitted when blank")
	}

	for key := range m {
		if !expectedKeys[key] {
			t.Errorf("Unexpected JSON field: %q", key)
		}
	}

	for key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("Missing JSON field: %q", key)
		}
	}
}

func TestEncodeManifestIncludesPreviousWhenSet(t *testing.T) {
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    2,
		Current:       revision.For([]byte("current")),
		Previous:      revision.For([]byte("previous")),
		ActivatedAt:   time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
	}

	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest() failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if got, ok := decoded["previous"]; !ok || got != manifest.Previous.String() {
		t.Errorf("previous = %v, present = %v, want %q", got, ok, manifest.Previous)
	}
}

func TestEncodeManifestIndented(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	manifest := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   now,
	}

	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest() failed: %v", err)
	}

	// Check for indentation (should have newlines and spaces)
	if !bytes.Contains(data, []byte("\n")) {
		t.Errorf("EncodeManifest() should produce indented JSON (has newlines)")
	}

	// Verify it's valid JSON when parsed back
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Errorf("Encoded data is not valid JSON: %v", err)
	}

	// Blank previous is omitted, so the manifest has four fields.
	if len(m) != 4 {
		t.Errorf("Encoded manifest has %d fields, want 4", len(m))
	}
}

func TestManifestValidateNoInputEcho(t *testing.T) {
	// Error messages should not echo input data
	invalidManifest := Manifest{
		FormatVersion: 999,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		Previous:      "",
		ActivatedAt:   time.Now(),
	}

	err := invalidManifest.Validate()
	if err == nil {
		t.Fatal("Validate() should fail")
	}

	errMsg := err.Error()
	// The error message should contain the code and field path,
	// but not echo back the manifest content itself
	if len(errMsg) == 0 {
		t.Error("Error message is empty")
	}

	// Verify structure: should be safe error, not dumping manifest
	if fmt.Sprintf("%#v", invalidManifest) == errMsg {
		t.Error("Error message echoed manifest contents")
	}
}

func TestDecodeManifestDuplicateGenerationKey(t *testing.T) {
	// Duplicate top-level keys should be rejected as CodeManifestCorrupt
	jsonWithDuplicate := []byte(`{
		"format_version": 1,
		"generation": 1,
		"generation": 2,
		"current": "rev123456789012345678901234",
		"activated_at": "2026-08-19T00:00:00Z"
	}`)

	_, err := DecodeManifest(jsonWithDuplicate)
	if err == nil {
		t.Fatal("DecodeManifest should reject duplicate keys")
	}

	if code, ok := CodeOf(err); !ok || code != CodeManifestCorrupt {
		t.Errorf("DecodeManifest error code = %q, present = %v, want %q", code, ok, CodeManifestCorrupt)
	}

	// Error message should not include the key name in error details
	errMsg := err.Error()
	if strings.Contains(errMsg, "generation") {
		t.Errorf("Error message should not echo key name: %q", errMsg)
	}
}

func TestDecodeManifestDuplicateCurrentKey(t *testing.T) {
	// Duplicate current keys should be rejected
	jsonWithDuplicate := []byte(`{
		"format_version": 1,
		"generation": 1,
		"current": "rev111111111111111111111111",
		"current": "rev222222222222222222222222",
		"activated_at": "2026-08-19T00:00:00Z"
	}`)

	_, err := DecodeManifest(jsonWithDuplicate)
	if err == nil {
		t.Fatal("DecodeManifest should reject duplicate current keys")
	}

	if code, ok := CodeOf(err); !ok || code != CodeManifestCorrupt {
		t.Errorf("DecodeManifest error code = %q, present = %v, want %q", code, ok, CodeManifestCorrupt)
	}
}

func TestManifestUTCNormalization(t *testing.T) {
	// Manifests representing the same instant in different zones should encode identically
	utcTime := time.Date(2026, 8, 19, 15, 30, 45, 0, time.UTC)
	estTime := time.Date(2026, 8, 19, 11, 30, 45, 0, time.FixedZone("EST", -4*3600))

	// These represent the same instant but in different zones
	if !utcTime.Equal(estTime) {
		t.Fatal("Test setup: times should be equal")
	}

	manifest1 := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		ActivatedAt:   utcTime,
	}

	manifest2 := Manifest{
		FormatVersion: ManifestFormatVersion,
		Generation:    1,
		Current:       revision.For([]byte("test")),
		ActivatedAt:   estTime,
	}

	data1, err := EncodeManifest(manifest1)
	if err != nil {
		t.Fatalf("EncodeManifest(utcTime) failed: %v", err)
	}

	data2, err := EncodeManifest(manifest2)
	if err != nil {
		t.Fatalf("EncodeManifest(estTime) failed: %v", err)
	}

	// Both should encode identically (with UTC Z notation)
	if !bytes.Equal(data1, data2) {
		t.Errorf("Different zone times encoded differently:\nUTC: %s\nEST: %s", data1, data2)
	}

	// Both should contain Z timezone indicator
	if !bytes.Contains(data1, []byte("Z")) {
		t.Errorf("Encoded manifest should use UTC Z notation, got: %s", data1)
	}
}

func TestDecodeManifestActivatedAtInUTC(t *testing.T) {
	// DecodeManifest should return ActivatedAt in UTC regardless of input zone
	validRev := revision.For([]byte("test"))
	jsonWithEST := []byte(`{
		"format_version": 1,
		"generation": 1,
		"current": "` + validRev.String() + `",
		"activated_at": "2026-08-19T15:30:45-04:00"
	}`)

	manifest, err := DecodeManifest(jsonWithEST)
	if err != nil {
		t.Fatalf("DecodeManifest failed: %v", err)
	}

	// Should be converted to UTC
	if manifest.ActivatedAt.Location() != time.UTC {
		t.Errorf("ActivatedAt location = %v, want %v", manifest.ActivatedAt.Location(), time.UTC)
	}

	// The wall clock time should represent the same instant
	expectedUTC := time.Date(2026, 8, 19, 19, 30, 45, 0, time.UTC)
	if !manifest.ActivatedAt.Equal(expectedUTC) {
		t.Errorf("ActivatedAt = %v, want %v", manifest.ActivatedAt, expectedUTC)
	}
}

func TestDecodeManifestMalformedJSONNoEcho(t *testing.T) {
	// Malformed JSON with secret-like field should not echo the secret
	malformed := []byte(`{
		"format_version": 1,
		"secret_key": "sk_live_abcdef123456",
		"current": invalid json here
	}`)

	_, err := DecodeManifest(malformed)
	if err == nil {
		t.Fatal("DecodeManifest should reject malformed JSON")
	}

	errMsg := err.Error()
	// Should not contain the secret value
	if strings.Contains(errMsg, "sk_live_") {
		t.Errorf("Error message leaked secret: %q", errMsg)
	}

	// Should contain a generic error message, not JSON text
	if strings.Contains(errMsg, "invalid json") {
		t.Errorf("Error message echoed malformed JSON: %q", errMsg)
	}
}
