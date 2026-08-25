//go:build windows

package runtimepaths

import (
	"os"
	"path/filepath"
)

func defaultStateRoot() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "ant-router"), nil
}

func defaultRuntimeRoot() (string, error) {
	return "", nil
}
