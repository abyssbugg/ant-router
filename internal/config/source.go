package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const ConfigEnvironment = "ANT_ROUTER_CONFIG"

// ResolveSource resolves the configuration source with precedence:
// 1. explicit (non-blank)
// 2. environment variable (non-blank)
// 3. defaultPath (non-blank)
//
// All paths are converted to absolute paths using filepath.Abs.
// If absolute resolution fails, returns a typed safe error.
// If all sources are blank, returns CodeSourceNotFound.
// nil getenv is treated as if the environment variable is not set.
func ResolveSource(explicit string, getenv func(string) string, defaultPath string) (string, error) {
	// Normalize whitespace-only strings to blank
	explicit = strings.TrimSpace(explicit)
	defaultPath = strings.TrimSpace(defaultPath)

	// Check explicit parameter first
	if explicit != "" {
		absPath, err := filepath.Abs(explicit)
		if err != nil {
			return "", &Error{
				Code: CodeDecodeFailed,
				Path: explicit,
				Err:  fmt.Errorf("cannot resolve absolute path"),
			}
		}
		return absPath, nil
	}

	// Check environment variable if getenv is not nil
	if getenv != nil {
		envVal := strings.TrimSpace(getenv(ConfigEnvironment))
		if envVal != "" {
			absPath, err := filepath.Abs(envVal)
			if err != nil {
				return "", &Error{
					Code: CodeDecodeFailed,
					Path: envVal,
					Err:  fmt.Errorf("cannot resolve absolute path"),
				}
			}
			return absPath, nil
		}
	}

	// Check default path
	if defaultPath != "" {
		absPath, err := filepath.Abs(defaultPath)
		if err != nil {
			return "", &Error{
				Code: CodeDecodeFailed,
				Path: defaultPath,
				Err:  fmt.Errorf("cannot resolve absolute path"),
			}
		}
		return absPath, nil
	}

	// All sources blank
	return "", &Error{
		Code: CodeSourceNotFound,
		Path: "",
		Err:  fmt.Errorf("no configuration source specified"),
	}
}
