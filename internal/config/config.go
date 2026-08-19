package config

import "time"

const (
	SchemaVersion          = 1
	DefaultShutdownTimeout = 30 * time.Second
)

// Config represents the root configuration structure.
// Direct construction with invalid values is defended by Validate().
type Config struct {
	SchemaVersion int          `toml:"schema_version"`
	Daemon        DaemonConfig `toml:"daemon"`
}

// DaemonConfig contains daemon-specific configuration
type DaemonConfig struct {
	ShutdownTimeout Duration `toml:"shutdown_timeout"`
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
