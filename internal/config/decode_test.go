package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMaxSourceBytesConstant(t *testing.T) {
	if MaxSourceBytes != (1 << 20) {
		t.Errorf("MaxSourceBytes = %d, want %d", MaxSourceBytes, 1<<20)
	}
}

func TestDecodeValidMinimal(t *testing.T) {
	toml := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
`
	r := strings.NewReader(toml)
	cfg, err := Decode(r)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want 6", cfg.SchemaVersion)
	}
	if cfg.Daemon.ShutdownTimeout.Duration != 30*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 30s", cfg.Daemon.ShutdownTimeout.Duration)
	}
}

func TestDecodeOmitDaemonUsesDefaults(t *testing.T) {
	// Omit daemon and timeout, should receive defaults
	toml := `schema_version = 1`
	r := strings.NewReader(toml)
	cfg, err := Decode(r)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want 6", cfg.SchemaVersion)
	}
	// Should have default timeout
	if cfg.Daemon.ShutdownTimeout.Duration != DefaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.Daemon.ShutdownTimeout.Duration, DefaultShutdownTimeout)
	}
}

func TestDecodeMissingSchemaVersion(t *testing.T) {
	toml := `[daemon]
shutdown_timeout = "30s"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for missing schema_version")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("got code %q, want validation_failed", code)
	}

	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatal("expected *Error type")
	}
	if configErr.Path != "schema_version" {
		t.Errorf("Path = %q, want \"schema_version\"", configErr.Path)
	}
}

func TestDecodeEmptyInput(t *testing.T) {
	r := strings.NewReader("")
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for empty input (missing schema_version)")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("got code %q, want validation_failed", code)
	}

	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatal("expected *Error type")
	}
	if configErr.Path != "schema_version" {
		t.Errorf("Path = %q, want \"schema_version\"", configErr.Path)
	}
}

func TestDecodeUnknownRootField(t *testing.T) {
	toml := `schema_version = 1
unknown_field = "value"
[daemon]
shutdown_timeout = "30s"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for unknown root field")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeUnknownField {
		t.Errorf("got code %q, want unknown_field", code)
	}
}

func TestDecodeUnknownDaemonField(t *testing.T) {
	toml := `schema_version = 1
[daemon]
shutdown_timeout = "30s"
unknown_daemon_field = "value"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for unknown daemon field")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeUnknownField {
		t.Errorf("got code %q, want unknown_field", code)
	}
}

func TestDecodeDuplicateKeys(t *testing.T) {
	toml := `schema_version = 1
schema_version = 2
[daemon]
shutdown_timeout = "30s"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for duplicate keys")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeDecodeFailed {
		t.Errorf("got code %q, want decode_failed", code)
	}
}

func TestDecodeMalformedTOML(t *testing.T) {
	toml := `schema_version = 1
[daemon
shutdown_timeout = "30s"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for malformed TOML")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeDecodeFailed {
		t.Errorf("got code %q, want decode_failed", code)
	}
}

func TestDecodeUnsupportedSchema(t *testing.T) {
	toml := `schema_version = 7
[daemon]
shutdown_timeout = "30s"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for unsupported schema version")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeUnsupportedSchema {
		t.Errorf("got code %q, want unsupported_schema", code)
	}
}

func TestDecodeInvalidDuration(t *testing.T) {
	toml := `schema_version = 1
[daemon]
shutdown_timeout = "invalid"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("got code %q, want validation_failed", code)
	}
}

func TestDecodeDurationOutOfBounds(t *testing.T) {
	toml := `schema_version = 1
[daemon]
shutdown_timeout = "100m"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for duration out of bounds")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("got code %q, want validation_failed", code)
	}
}

func TestDecodeSourceTooLarge(t *testing.T) {
	// Create input larger than MaxSourceBytes
	tooLarge := strings.Repeat("x", MaxSourceBytes+1)
	r := strings.NewReader(tooLarge)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for oversized input")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeSourceTooLarge {
		t.Errorf("got code %q, want source_too_large", code)
	}
}

func TestDecodeFileNotFound(t *testing.T) {
	_, err := DecodeFile("/nonexistent/path/config.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeSourceNotFound {
		t.Errorf("got code %q, want source_not_found", code)
	}

	// Verify error includes the file path
	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatal("expected *Error type")
	}
	if configErr.Path == "" {
		t.Errorf("Path should be set, got empty")
	}
}

func TestDecodeErrorDoesNotEchoSecrets(t *testing.T) {
	toml := `schema_version = 1
api_key = "secret_password_123"
[daemon]
shutdown_timeout = "30s"
`
	r := strings.NewReader(toml)
	_, err := Decode(r)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}

	errMsg := err.Error()
	// Error message must not contain sensitive values
	if strings.Contains(errMsg, "secret_password_123") {
		t.Error("error message contains secret value")
	}
	if strings.Contains(errMsg, "api_key") {
		// api_key is acceptable as field name context, but not the value
		if strings.Contains(errMsg, "secret") {
			t.Error("error message contains secret value context")
		}
	}
}

func TestDecodeValidFull(t *testing.T) {
	toml := `schema_version = 1
[daemon]
shutdown_timeout = "45s"
`
	r := strings.NewReader(toml)
	cfg, err := Decode(r)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want 6", cfg.SchemaVersion)
	}
	if cfg.Daemon.ShutdownTimeout.Duration != 45*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 45s", cfg.Daemon.ShutdownTimeout.Duration)
	}
}

func TestDecodeFileSuccess(t *testing.T) {
	// Use relative path from internal/config directory
	testfile := "testdata/valid-minimal.toml"

	cfg, err := DecodeFile(testfile)
	if err != nil {
		t.Fatalf("DecodeFile failed: %v", err)
	}

	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want 6", cfg.SchemaVersion)
	}
	if cfg.Daemon.ShutdownTimeout.Duration != DefaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.Daemon.ShutdownTimeout.Duration, DefaultShutdownTimeout)
	}
}
