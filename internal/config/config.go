package config

import "time"

const (
	LegacySchemaVersion           = 1
	PreviousSchemaVersion         = 2
	ProviderRegistrySchemaVersion = 3
	AnthropicSchemaVersion        = 4
	CodexSchemaVersion            = 5
	SchemaVersion                 = 6
	DefaultShutdownTimeout        = 30 * time.Second
)

// Config represents the root configuration structure.
// Direct construction with invalid values is defended by Validate().
type Config struct {
	SchemaVersion int              `toml:"schema_version"`
	Daemon        DaemonConfig     `toml:"daemon"`
	DataPlane     DataPlaneConfig  `toml:"data_plane"`
	Providers     []ProviderConfig `toml:"providers,omitempty"`
	// MockProvider is an unencoded compatibility shadow removed after daemon registry wiring.
	MockProvider MockProviderConfig `toml:"-"`
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
		DataPlane: defaultDataPlaneConfig(),
	}
}
