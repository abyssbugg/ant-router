package config

import (
	"fmt"
	"time"
)

const (
	SchemaVersion          = 1
	DefaultShutdownTimeout = 30 * time.Second
)

// Config represents the root configuration structure.
// Direct construction with invalid values is defended by Validate().
type Config struct {
	SchemaVersion int
	Daemon        DaemonConfig
}

// DaemonConfig contains daemon-specific configuration
type DaemonConfig struct {
	ShutdownTimeout Duration
}

// Default returns a Config with default values
func Default() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Daemon: DaemonConfig{
			ShutdownTimeout: Duration{DefaultShutdownTimeout},
		},
	}
}

// Validate checks if the configuration is valid.
// This defends against direct construction with invalid values,
// ensuring that both programmatic and deserialized configs are validated.
func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return &Error{
			Code: CodeUnsupportedSchema,
			Path: "schema_version",
			Err:  fmt.Errorf("unsupported schema version %d", c.SchemaVersion),
		}
	}

	// Validate ShutdownTimeout using centralized bounds checking
	if valErr := validateDurationBounds(c.Daemon.ShutdownTimeout.Duration, "daemon.shutdown_timeout"); valErr != nil {
		return valErr
	}

	return nil
}
