package config

import (
	"fmt"
	"time"
)

// Duration bounds defined once for reuse
const (
	minDuration = 1 * time.Second
	maxDuration = 10 * time.Minute
)

// Duration wraps time.Duration with validation
type Duration struct {
	time.Duration
}

// validateDurationBounds checks if a parsed duration is within acceptable bounds
// and returns an error with the given path context if validation fails.
// This helper is used by both ParseDuration and Config.Validate to preserve path context.
func validateDurationBounds(d time.Duration, path string) error {
	if d == 0 {
		return &Error{
			Code: CodeValidationFailed,
			Path: path,
			Err:  fmt.Errorf("duration cannot be zero"),
		}
	}

	if d < 0 {
		return &Error{
			Code: CodeValidationFailed,
			Path: path,
			Err:  fmt.Errorf("duration cannot be negative"),
		}
	}

	if d < minDuration {
		return &Error{
			Code: CodeValidationFailed,
			Path: path,
			Err:  fmt.Errorf("duration must be at least %v", minDuration),
		}
	}

	if d > maxDuration {
		return &Error{
			Code: CodeValidationFailed,
			Path: path,
			Err:  fmt.Errorf("duration cannot exceed %v", maxDuration),
		}
	}

	return nil
}

// ParseDuration parses a duration string with validation.
// Does not wrap time.ParseDuration errors to avoid exposing raw input in error messages.
func ParseDuration(s string) (Duration, error) {
	if s == "" {
		return Duration{}, &Error{
			Code: CodeValidationFailed,
			Path: "",
			Err:  fmt.Errorf("duration cannot be empty"),
		}
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		// Do not wrap time.ParseDuration error as it contains raw input
		return Duration{}, &Error{
			Code: CodeValidationFailed,
			Path: "",
			Err:  fmt.Errorf("invalid duration format"),
		}
	}

	// Centralized bounds checking
	if valErr := validateDurationBounds(d, ""); valErr != nil {
		return Duration{}, valErr
	}

	return Duration{d}, nil
}

// String returns the string representation of Duration
func (d Duration) String() string {
	return d.Duration.String()
}

// MarshalText implements encoding.TextMarshaler
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler
func (d *Duration) UnmarshalText(data []byte) error {
	parsed, err := ParseDuration(string(data))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
