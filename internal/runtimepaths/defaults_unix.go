//go:build !windows

package runtimepaths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func defaultStateRoot() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "ant-router"), nil
}

func defaultRuntimeRoot() (string, error) {
	if runtime.GOOS == "linux" {
		if root := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); root != "" {
			return root, nil
		}
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("ant-router-%d", os.Getuid())), nil
}
