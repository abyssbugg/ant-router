package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

const MaxSourceBytes = 1 << 20 // 1 MiB

// wireConfig is a private type for TOML decoding with optional/pointer fields
type wireConfig struct {
	SchemaVersion *int        `toml:"schema_version"`
	Daemon        *wireDaemon `toml:"daemon"`
	_             struct{}    `toml:"" json:"-"` // Catch unknown fields at root
}

// wireDaemon is a private type for daemon config decoding
type wireDaemon struct {
	ShutdownTimeout *string  `toml:"shutdown_timeout"`
	_               struct{} `toml:"" json:"-"` // Catch unknown fields
}

// Decode decodes a Config from a TOML reader with strict validation.
// Starts from Default() so omitted fields receive default values.
// Rejects unknown fields, duplicate keys, malformed TOML, unsupported schema,
// semantic timeout failures, and input larger than MaxSourceBytes.
// Requires schema_version to be present.
func Decode(r io.Reader) (Config, error) {
	// Read with size limit to prevent memory exhaustion
	lr := io.LimitReader(r, int64(MaxSourceBytes)+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return Config{}, &Error{
			Code: CodeDecodeFailed,
			Path: "",
			Err:  fmt.Errorf("failed to read source"),
		}
	}

	if len(data) > MaxSourceBytes {
		return Config{}, &Error{
			Code: CodeSourceTooLarge,
			Path: "",
			Err:  fmt.Errorf("source exceeds maximum size of %d bytes", MaxSourceBytes),
		}
	}

	// Start from Default() for proper defaults
	cfg := Default()

	// Empty input requires schema_version
	if len(data) == 0 {
		return Config{}, &Error{
			Code: CodeValidationFailed,
			Path: "schema_version",
			Err:  fmt.Errorf("schema_version is required"),
		}
	}

	// Parse TOML with strict decoding into wire type
	wire := &wireConfig{}
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields() // Reject unknown fields

	err = decoder.Decode(wire)
	if err != nil {
		// Classify error by type
		var strictErr *toml.StrictMissingError
		if errors.As(err, &strictErr) {
			return Config{}, &Error{
				Code: CodeUnknownField,
				Path: "",
				Err:  fmt.Errorf("unknown configuration field"),
			}
		}

		// All other decoder errors are syntax/format errors
		return Config{}, &Error{
			Code: CodeDecodeFailed,
			Path: "",
			Err:  fmt.Errorf("invalid TOML format"),
		}
	}

	// Require schema_version to be present
	if wire.SchemaVersion == nil {
		return Config{}, &Error{
			Code: CodeValidationFailed,
			Path: "schema_version",
			Err:  fmt.Errorf("schema_version is required"),
		}
	}

	// Overlay schema_version
	cfg.SchemaVersion = *wire.SchemaVersion

	// Overlay daemon config if present
	if wire.Daemon != nil {
		if wire.Daemon.ShutdownTimeout != nil {
			// Parse duration with path context
			parsed, err := ParseDuration(*wire.Daemon.ShutdownTimeout)
			if err != nil {
				// Wrap the error with TOML path context
				var cfgErr *Error
				if errors.As(err, &cfgErr) {
					cfgErr.Path = "daemon.shutdown_timeout"
					return Config{}, cfgErr
				}
				return Config{}, &Error{
					Code: CodeDecodeFailed,
					Path: "daemon.shutdown_timeout",
					Err:  fmt.Errorf("invalid duration format"),
				}
			}
			cfg.Daemon.ShutdownTimeout = parsed
		}
	}

	// Validate the final configuration
	if valErr := cfg.Validate(); valErr != nil {
		return Config{}, valErr
	}

	return cfg, nil
}

// DecodeFile decodes a Config from a file path.
// Maps missing file to CodeSourceNotFound with the safe source path.
// Does not echo full TOML or secret-like invalid values in errors.
func DecodeFile(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			cleanPath := filepath.Clean(path)
			return Config{}, &Error{
				Code: CodeSourceNotFound,
				Path: cleanPath,
				Err:  fmt.Errorf("configuration file not found"),
			}
		}
		// Other OS errors
		cleanPath := filepath.Clean(path)
		return Config{}, &Error{
			Code: CodeDecodeFailed,
			Path: cleanPath,
			Err:  fmt.Errorf("cannot read configuration file"),
		}
	}
	defer file.Close()

	cfg, err := Decode(file)
	if err != nil {
		// Preserve original error but ensure path is set if it's a source_not_found
		if cfgErr, isConfigErr := err.(*Error); isConfigErr {
			cleanPath := filepath.Clean(path)
			if cfgErr.Path == "" {
				cfgErr.Path = cleanPath
			}
			return Config{}, cfgErr
		}
		return Config{}, err
	}

	return cfg, nil
}
