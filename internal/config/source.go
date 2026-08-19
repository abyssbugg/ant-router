package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const ConfigEnvironment = "ANT_ROUTER_CONFIG"

// resolveAbsolutePath converts a path to an absolute, cleaned path.
// If the path is blank, it returns blank and nil error.
// If filepath.Abs fails, it returns an error with CodePathResolutionFailed.
// The field parameter is used in the error Path field to identify the source.
func resolveAbsolutePath(path, field string) (string, error) {
	// Blank paths remain blank
	if path == "" {
		return "", nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", &Error{
			Code: CodePathResolutionFailed,
			Path: field,
			Err:  fmt.Errorf("cannot resolve absolute path: %v", err),
		}
	}

	return filepath.Clean(absPath), nil
}

// ResolveSource resolves the configuration source with precedence:
// 1. explicit (non-blank)
// 2. environment variable (non-blank)
// 3. defaultPath (non-blank)
//
// All paths are converted to absolute paths using resolveAbsolutePath.
// If absolute resolution fails, returns CodePathResolutionFailed error with field "config_path".
// If all sources are blank, returns CodeSourceNotFound.
// nil getenv is treated as if the environment variable is not set.
func ResolveSource(explicit string, getenv func(string) string, defaultPath string) (string, error) {
	// Normalize whitespace-only strings to blank
	explicit = strings.TrimSpace(explicit)
	defaultPath = strings.TrimSpace(defaultPath)

	// Check explicit parameter first
	if explicit != "" {
		absPath, err := resolveAbsolutePath(explicit, "config_path")
		if err != nil {
			return "", err
		}
		return absPath, nil
	}

	// Check environment variable if getenv is not nil
	if getenv != nil {
		envVal := strings.TrimSpace(getenv(ConfigEnvironment))
		if envVal != "" {
			absPath, err := resolveAbsolutePath(envVal, "config_path")
			if err != nil {
				return "", err
			}
			return absPath, nil
		}
	}

	// Check default path
	if defaultPath != "" {
		absPath, err := resolveAbsolutePath(defaultPath, "config_path")
		if err != nil {
			return "", err
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
