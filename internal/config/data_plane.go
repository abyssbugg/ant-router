package config

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

const (
	DefaultDataPlaneListen          = "127.0.0.1:8787"
	DefaultMaxConcurrentRequests    = 32
	MaxDataPlaneConcurrentRequests  = 1024
	DefaultDataPlaneShutdownTimeout = 30 * time.Second
)

// DataPlaneConfig configures the local OpenAI-compatible inference listener.
type DataPlaneConfig struct {
	Enabled               bool     `toml:"enabled"`
	Listen                string   `toml:"listen"`
	MaxConcurrentRequests int      `toml:"max_concurrent_requests"`
	ShutdownTimeout       Duration `toml:"shutdown_timeout"`
}

// MockProviderConfig controls the explicitly enabled deterministic mock provider.
type MockProviderConfig struct {
	Enabled bool `toml:"enabled"`
}

func defaultDataPlaneConfig() DataPlaneConfig {
	return DataPlaneConfig{
		Listen:                DefaultDataPlaneListen,
		MaxConcurrentRequests: DefaultMaxConcurrentRequests,
		ShutdownTimeout:       Duration{DefaultDataPlaneShutdownTimeout},
	}
}

func (c Config) normalized() Config {
	if c.v5Migrated {
		c = completeV5CodexBaselines(c)
		c.v5Migrated = false
	}
	if c.DataPlane == (DataPlaneConfig{}) {
		c.DataPlane = defaultDataPlaneConfig()
	}
	return normalizeProviders(c)
}

func validateDataPlane(c Config) error {
	host, portText, err := net.SplitHostPort(c.DataPlane.Listen)
	if err != nil || host != "127.0.0.1" {
		return &Error{Code: CodeValidationFailed, Path: "data_plane.listen", Err: fmt.Errorf("must be an IPv4 loopback address with a port")}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return &Error{Code: CodeValidationFailed, Path: "data_plane.listen", Err: fmt.Errorf("port must be between 1 and 65535")}
	}
	if c.DataPlane.MaxConcurrentRequests < 1 || c.DataPlane.MaxConcurrentRequests > MaxDataPlaneConcurrentRequests {
		return &Error{Code: CodeValidationFailed, Path: "data_plane.max_concurrent_requests", Err: fmt.Errorf("must be between 1 and %d", MaxDataPlaneConcurrentRequests)}
	}
	if c.DataPlane.ShutdownTimeout.Duration < time.Second || c.DataPlane.ShutdownTimeout.Duration > 5*time.Minute {
		return &Error{Code: CodeValidationFailed, Path: "data_plane.shutdown_timeout", Err: fmt.Errorf("must be between 1s and 5m")}
	}
	if c.DataPlane.Enabled {
		enabled := false
		for _, provider := range c.Providers {
			enabled = enabled || provider.Enabled
		}
		if !enabled {
			return &Error{Code: CodeValidationFailed, Path: "providers", Err: fmt.Errorf("at least one provider must be enabled for the configured data plane")}
		}
	}
	return nil
}

// ValidateForInstance validates the configuration plus named-instance port isolation.
func (c Config) ValidateForInstance(instance string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.DataPlane.Enabled && instance != "default" && c.DataPlane.Listen == DefaultDataPlaneListen {
		return &Error{Code: CodeValidationFailed, Path: "data_plane.listen", Err: fmt.Errorf("named instances require a distinct port")}
	}
	return nil
}
