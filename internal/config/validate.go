package config

import "fmt"

// Validate checks if the configuration is valid.
// This defends against direct construction with invalid values,
// ensuring that both programmatic and deserialized configs are validated.
func (c Config) Validate() error {
	c = c.normalized()
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
	if valErr := validateDataPlane(c); valErr != nil {
		return valErr
	}

	return nil
}
