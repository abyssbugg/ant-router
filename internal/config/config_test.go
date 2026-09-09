package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSchemaVersionConstant(t *testing.T) {
	if SchemaVersion != 6 {
		t.Errorf("SchemaVersion = %d, want 6", SchemaVersion)
	}
}

func TestDefaultShutdownTimeout(t *testing.T) {
	if DefaultShutdownTimeout != 30*time.Second {
		t.Errorf("DefaultShutdownTimeout = %v, want 30s", DefaultShutdownTimeout)
	}
}

func TestConfigType(t *testing.T) {
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}

	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("Config.SchemaVersion = %d, want 6", cfg.SchemaVersion)
	}
}

func TestDaemonConfigType(t *testing.T) {
	dc := DaemonConfig{
		ShutdownTimeout: Duration{30 * time.Second},
	}

	if dc.ShutdownTimeout.Duration != 30*time.Second {
		t.Errorf("DaemonConfig.ShutdownTimeout = %v, want 30s", dc.ShutdownTimeout.Duration)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("Default().SchemaVersion = %d, want 6", cfg.SchemaVersion)
	}

	if cfg.Daemon.ShutdownTimeout != (Duration{30 * time.Second}) {
		t.Errorf("Default().Daemon.ShutdownTimeout = %v, want 30s", cfg.Daemon.ShutdownTimeout)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errCode Code
		errPath string
	}{
		{
			name: "valid default config",
			config: Config{
				SchemaVersion: SchemaVersion,
				Daemon: DaemonConfig{
					ShutdownTimeout: Duration{30 * time.Second},
				},
			},
			wantErr: false,
		},
		{
			name: "valid minimum timeout",
			config: Config{
				SchemaVersion: SchemaVersion,
				Daemon: DaemonConfig{
					ShutdownTimeout: Duration{1 * time.Second},
				},
			},
			wantErr: false,
		},
		{
			name: "valid maximum timeout",
			config: Config{
				SchemaVersion: SchemaVersion,
				Daemon: DaemonConfig{
					ShutdownTimeout: Duration{10 * time.Minute},
				},
			},
			wantErr: false,
		},
		{
			name: "unsupported schema version 0",
			config: Config{
				SchemaVersion: 0,
				Daemon: DaemonConfig{
					ShutdownTimeout: Duration{30 * time.Second},
				},
			},
			wantErr: true,
			errCode: CodeUnsupportedSchema,
			errPath: "schema_version",
		},
		{
			name: "unsupported schema version 7",
			config: Config{
				SchemaVersion: 7,
				Daemon: DaemonConfig{
					ShutdownTimeout: Duration{30 * time.Second},
				},
			},
			wantErr: true,
			errCode: CodeUnsupportedSchema,
			errPath: "schema_version",
		},
		{
			name: "timeout below minimum",
			config: Config{
				SchemaVersion: SchemaVersion,
				Daemon: DaemonConfig{
					ShutdownTimeout: Duration{500 * time.Millisecond},
				},
			},
			wantErr: true,
			errCode: CodeValidationFailed,
			errPath: "daemon.shutdown_timeout",
		},
		{
			name: "timeout above maximum",
			config: Config{
				SchemaVersion: SchemaVersion,
				Daemon: DaemonConfig{
					ShutdownTimeout: Duration{11 * time.Minute},
				},
			},
			wantErr: true,
			errCode: CodeValidationFailed,
			errPath: "daemon.shutdown_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				var configErr *Error
				if !errors.As(err, &configErr) {
					t.Errorf("Expected *Error type, got %T", err)
					return
				}

				if configErr.Code != tt.errCode {
					t.Errorf("Validate() code = %v, want %v", configErr.Code, tt.errCode)
				}

				if configErr.Path != tt.errPath {
					t.Errorf("Validate() path = %v, want %v", configErr.Path, tt.errPath)
				}
			}
		})
	}
}

func TestConfigValidateErrorMessage(t *testing.T) {
	cfg := Config{
		SchemaVersion: 7,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error")
	}

	errMsg := err.Error()
	// Error message should not contain config source content
	if errMsg == "" {
		t.Error("Error message is empty")
	}
	// Verify it doesn't expose internal values
	if strings.Contains(errMsg, "SchemaVersion: 7") {
		t.Error("Error message should not contain config source content")
	}
}

func TestDefaultValidates(t *testing.T) {
	cfg := Default()
	err := cfg.Validate()
	if err != nil {
		t.Errorf("Default() config should be valid, got error: %v", err)
	}
}

func TestConfigValidateErrorsIs(t *testing.T) {
	cfg := Config{
		SchemaVersion: 0,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error")
	}

	code, ok := CodeOf(err)
	if !ok {
		t.Error("Expected CodeOf to extract code")
	}
	if code != CodeUnsupportedSchema {
		t.Errorf("CodeOf() = %v, want %v", code, CodeUnsupportedSchema)
	}
}

func TestDirectInvalidStructLiteralRejected(t *testing.T) {
	// A direct invalid struct literal should be rejected by Config.Validate
	cfg := Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{500 * time.Millisecond},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for direct invalid struct literal")
	}

	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("Expected CodeValidationFailed, got %v", code)
	}

	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatal("Expected *Error type")
	}
	if configErr.Path != "daemon.shutdown_timeout" {
		t.Errorf("Path = %v, want 'daemon.shutdown_timeout'", configErr.Path)
	}
}

func TestConfigValidateErrorCodeLowercase(t *testing.T) {
	cfg := Config{
		SchemaVersion: 0,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{30 * time.Second},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error")
	}

	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatal("Expected *Error type")
	}

	if configErr.Code != "unsupported_schema" {
		t.Errorf("Error code = %v, want 'unsupported_schema'", configErr.Code)
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "unsupported_schema") {
		t.Errorf("Error message should contain lowercase code: %v", errMsg)
	}
}
