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
	SchemaVersion *int              `toml:"schema_version"`
	Daemon        *wireDaemon       `toml:"daemon"`
	DataPlane     *wireDataPlane    `toml:"data_plane"`
	MockProvider  *wireMockProvider `toml:"mock_provider"`
	Providers     []wireProvider    `toml:"providers"`
	_             struct{}          `toml:"" json:"-"` // Catch unknown fields at root
}

// wireDaemon is a private type for daemon config decoding
type wireDaemon struct {
	ShutdownTimeout *string  `toml:"shutdown_timeout"`
	_               struct{} `toml:"" json:"-"` // Catch unknown fields
}

type wireDataPlane struct {
	Enabled               *bool    `toml:"enabled"`
	Listen                *string  `toml:"listen"`
	MaxConcurrentRequests *int     `toml:"max_concurrent_requests"`
	ShutdownTimeout       *string  `toml:"shutdown_timeout"`
	_                     struct{} `toml:"" json:"-"`
}

type wireMockProvider struct {
	Enabled *bool    `toml:"enabled"`
	_       struct{} `toml:"" json:"-"`
}

type wireProvider struct {
	ID         *string             `toml:"id"`
	Kind       *string             `toml:"kind"`
	Enabled    *bool               `toml:"enabled"`
	Credential *wireCredential     `toml:"credential"`
	Models     []wireProviderModel `toml:"models"`
	_          struct{}            `toml:"" json:"-"`
}

type wireCredential struct {
	Source *string  `toml:"source"`
	Name   *string  `toml:"name"`
	_      struct{} `toml:"" json:"-"`
}

type wireProviderModel struct {
	Alias                  *string    `toml:"alias"`
	Upstream               *string    `toml:"upstream"`
	Capabilities           *[]string  `toml:"capabilities"`
	DefaultMaxOutputTokens *int64     `toml:"default_max_output_tokens"`
	MaxOutputTokens        *int64     `toml:"max_output_tokens"`
	Codex                  *wireCodex `toml:"codex"`
	_                      struct{}   `toml:"" json:"-"`
}

type wireCodex struct {
	Enabled                       *bool    `toml:"enabled"`
	DisplayName                   *string  `toml:"display_name"`
	Description                   *string  `toml:"description"`
	ContextWindow                 *int64   `toml:"context_window"`
	EffectiveContextWindowPercent *int     `toml:"effective_context_window_percent"`
	Priority                      *int     `toml:"priority"`
	Visibility                    *string  `toml:"visibility"`
	_                             struct{} `toml:"" json:"-"`
}

func rejectProviderCapabilityBeforeVersion(providers []wireProvider, capability string, version int) error {
	for i, provider := range providers {
		for j, model := range provider.Models {
			if model.Capabilities == nil {
				continue
			}
			for k, candidate := range *model.Capabilities {
				if candidate == capability {
					return &Error{Code: CodeValidationFailed, Path: fmt.Sprintf("providers[%d].models[%d].capabilities[%d]", i, j, k), Err: fmt.Errorf("capability is unavailable in schema version %d", version)}
				}
			}
		}
	}
	return nil
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
			Err:  errors.New("invalid TOML format"),
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

	// Accept legacy v1-v5 as input and normalize every in-memory config to v6.
	switch *wire.SchemaVersion {
	case LegacySchemaVersion:
		if wire.DataPlane != nil {
			return Config{}, &Error{Code: CodeUnknownField, Path: "data_plane", Err: fmt.Errorf("field is unavailable in schema version 1")}
		}
		if wire.MockProvider != nil {
			return Config{}, &Error{Code: CodeUnknownField, Path: "mock_provider", Err: fmt.Errorf("field is unavailable in schema version 1")}
		}
		if wire.Providers != nil {
			return Config{}, &Error{Code: CodeUnknownField, Path: "providers", Err: fmt.Errorf("field is unavailable in schema version 1")}
		}
	case PreviousSchemaVersion:
		if wire.Providers != nil {
			return Config{}, &Error{Code: CodeUnknownField, Path: "providers", Err: fmt.Errorf("field is unavailable in schema version 2")}
		}
	case ProviderRegistrySchemaVersion:
		if wire.MockProvider != nil {
			return Config{}, &Error{Code: CodeUnknownField, Path: "mock_provider", Err: fmt.Errorf("field is unavailable in schema version 3")}
		}
		if err := rejectProviderCapabilityBeforeVersion(wire.Providers, "custom_tools", ProviderRegistrySchemaVersion); err != nil {
			return Config{}, err
		}
		for i, provider := range wire.Providers {
			if provider.Kind != nil && ProviderKind(*provider.Kind) == ProviderKindAnthropic {
				return Config{}, &Error{Code: CodeValidationFailed, Path: fmt.Sprintf("providers[%d].kind", i), Err: fmt.Errorf("provider kind is unavailable in schema version 3")}
			}
			for j, model := range provider.Models {
				if model.Codex != nil {
					return Config{}, &Error{Code: CodeUnknownField, Path: fmt.Sprintf("providers[%d].models[%d].codex", i, j), Err: fmt.Errorf("field is unavailable in schema version 3")}
				}
				if model.DefaultMaxOutputTokens != nil {
					return Config{}, &Error{Code: CodeUnknownField, Path: fmt.Sprintf("providers[%d].models[%d].default_max_output_tokens", i, j), Err: fmt.Errorf("field is unavailable in schema version 3")}
				}
				if model.MaxOutputTokens != nil {
					return Config{}, &Error{Code: CodeUnknownField, Path: fmt.Sprintf("providers[%d].models[%d].max_output_tokens", i, j), Err: fmt.Errorf("field is unavailable in schema version 3")}
				}
			}
		}
	case AnthropicSchemaVersion:
		if wire.MockProvider != nil {
			return Config{}, &Error{Code: CodeUnknownField, Path: "mock_provider", Err: fmt.Errorf("field is unavailable in schema version 4")}
		}
		if err := rejectProviderCapabilityBeforeVersion(wire.Providers, "custom_tools", AnthropicSchemaVersion); err != nil {
			return Config{}, err
		}
		for i, provider := range wire.Providers {
			for j, model := range provider.Models {
				if model.Codex != nil {
					return Config{}, &Error{Code: CodeUnknownField, Path: fmt.Sprintf("providers[%d].models[%d].codex", i, j), Err: fmt.Errorf("field is unavailable in schema version 4")}
				}
			}
		}
	case CodexSchemaVersion:
		if wire.MockProvider != nil {
			return Config{}, &Error{Code: CodeUnknownField, Path: "mock_provider", Err: fmt.Errorf("field is unavailable in schema version 5")}
		}
		if err := rejectProviderCapabilityBeforeVersion(wire.Providers, "custom_tools", CodexSchemaVersion); err != nil {
			return Config{}, err
		}
	case SchemaVersion:
		if wire.MockProvider != nil {
			return Config{}, &Error{Code: CodeUnknownField, Path: "mock_provider", Err: fmt.Errorf("field is unavailable in schema version 6")}
		}
	default:
		return Config{}, &Error{Code: CodeUnsupportedSchema, Path: "schema_version", Err: fmt.Errorf("unsupported schema version %d", *wire.SchemaVersion)}
	}

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

	if wire.DataPlane != nil {
		if wire.DataPlane.Enabled != nil {
			cfg.DataPlane.Enabled = *wire.DataPlane.Enabled
		}
		if wire.DataPlane.Listen != nil {
			cfg.DataPlane.Listen = *wire.DataPlane.Listen
		}
		if wire.DataPlane.MaxConcurrentRequests != nil {
			cfg.DataPlane.MaxConcurrentRequests = *wire.DataPlane.MaxConcurrentRequests
		}
		if wire.DataPlane.ShutdownTimeout != nil {
			parsed, parseErr := ParseDuration(*wire.DataPlane.ShutdownTimeout)
			if parseErr != nil {
				return Config{}, &Error{Code: CodeDecodeFailed, Path: "data_plane.shutdown_timeout", Err: fmt.Errorf("invalid duration format")}
			}
			cfg.DataPlane.ShutdownTimeout = parsed
		}
	}
	if *wire.SchemaVersion == PreviousSchemaVersion && wire.MockProvider != nil && wire.MockProvider.Enabled != nil && *wire.MockProvider.Enabled {
		cfg.Providers = []ProviderConfig{{ID: "mock", Kind: ProviderKindMock, Enabled: true}}
	}
	if *wire.SchemaVersion == ProviderRegistrySchemaVersion || *wire.SchemaVersion == AnthropicSchemaVersion || *wire.SchemaVersion == CodexSchemaVersion || *wire.SchemaVersion == SchemaVersion {
		if wire.Providers != nil {
			cfg.Providers = make([]ProviderConfig, len(wire.Providers))
		}
		for i, provider := range wire.Providers {
			converted := ProviderConfig{}
			if provider.ID != nil {
				converted.ID = *provider.ID
			}
			if provider.Kind != nil {
				converted.Kind = ProviderKind(*provider.Kind)
			}
			if provider.Enabled != nil {
				converted.Enabled = *provider.Enabled
			}
			if provider.Credential != nil {
				converted.Credential = &CredentialRef{}
				if provider.Credential.Source != nil {
					converted.Credential.Source = CredentialSourceKind(*provider.Credential.Source)
				}
				if provider.Credential.Name != nil {
					converted.Credential.Name = *provider.Credential.Name
				}
			}
			if provider.Models != nil {
				converted.Models = make([]ProviderModelConfig, len(provider.Models))
			}
			for j, model := range provider.Models {
				if model.Alias != nil {
					converted.Models[j].Alias = *model.Alias
				}
				if model.Upstream != nil {
					converted.Models[j].Upstream = *model.Upstream
				}
				if model.Capabilities != nil {
					converted.Models[j].Capabilities = append([]string(nil), (*model.Capabilities)...)
				}
				if model.DefaultMaxOutputTokens != nil {
					converted.Models[j].DefaultMaxOutputTokens = *model.DefaultMaxOutputTokens
				}
				if model.MaxOutputTokens != nil {
					converted.Models[j].MaxOutputTokens = *model.MaxOutputTokens
				}
				if model.Codex != nil {
					codex := &CodexModelConfig{}
					if model.Codex.Enabled != nil {
						codex.Enabled = *model.Codex.Enabled
					}
					if model.Codex.DisplayName != nil {
						codex.DisplayName = *model.Codex.DisplayName
					}
					if model.Codex.Description != nil {
						codex.Description = *model.Codex.Description
					}
					if model.Codex.ContextWindow != nil {
						codex.ContextWindow = *model.Codex.ContextWindow
					}
					if model.Codex.EffectiveContextWindowPercent != nil {
						codex.EffectiveContextWindowPercent = *model.Codex.EffectiveContextWindowPercent
					}
					if model.Codex.Priority != nil {
						codex.Priority = *model.Codex.Priority
					}
					if model.Codex.Visibility != nil {
						codex.Visibility = CodexVisibility(*model.Codex.Visibility)
					}
					converted.Models[j].Codex = codex
				}
			}
			cfg.Providers[i] = converted
		}
	}

	cfg = cfg.normalized()
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
	defer func() { _ = file.Close() }()

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
