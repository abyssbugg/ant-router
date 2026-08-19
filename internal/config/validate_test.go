package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidateValidConfig(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{DefaultShutdownTimeout},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate failed on valid config: %v", err)
	}
}

func TestValidateValidCustomDuration(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{5 * time.Second},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate failed on valid config with custom duration: %v", err)
	}
}

func TestValidateUnsupportedSchemaVersion(t *testing.T) {
	cfg := Config{
		SchemaVersion: 0,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{DefaultShutdownTimeout},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for unsupported schema version")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeUnsupportedSchema {
		t.Errorf("got code %q, want unsupported_schema", code)
	}
}

func TestValidateTimeoutBelowMinimum(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{500 * time.Millisecond},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for timeout below minimum")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("got code %q, want validation_failed", code)
	}
}

func TestValidateTimeoutAboveMaximum(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{11 * time.Minute},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for timeout above maximum")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("got code %q, want validation_failed", code)
	}
}

func TestValidateTimeoutAtMinimumBoundary(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{1 * time.Second},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate failed at minimum boundary: %v", err)
	}
}

func TestValidateTimeoutAtMaximumBoundary(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{10 * time.Minute},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate failed at maximum boundary: %v", err)
	}
}

func TestValidateErrorMessagePath(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{500 * time.Millisecond},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	if !strings.Contains(err.Error(), "daemon.shutdown_timeout") {
		t.Errorf("error message should contain path context")
	}
}

func TestValidateErrorMessageContent(t *testing.T) {
	cfg := Config{
		SchemaVersion: 0,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{DefaultShutdownTimeout},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for unsupported schema version")
	}
	errMsg := err.Error()

	// Should contain code and path
	if !strings.Contains(errMsg, "unsupported_schema") {
		t.Errorf("error message should contain code")
	}
	if !strings.Contains(errMsg, "schema_version") {
		t.Errorf("error message should contain path")
	}
}

func TestValidateTimeoutZero(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{0},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for zero timeout")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("got code %q, want validation_failed", code)
	}
}

func TestValidateTimeoutNegative(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{-5 * time.Second},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative timeout")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("got code %q, want validation_failed", code)
	}
}
