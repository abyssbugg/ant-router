package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// logDirComponents returns the path components of logDir below stateRoot.
// The service log directory must be strictly inside the state directory, so
// its privacy is anchored to a directory the user chose for ant-router.
func logDirComponents(stateRoot, logDir string) ([]string, error) {
	if !filepath.IsAbs(stateRoot) || !filepath.IsAbs(logDir) {
		return nil, fmt.Errorf("log and state directories must be absolute")
	}
	rel, err := filepath.Rel(filepath.Clean(stateRoot), filepath.Clean(logDir))
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("log directory %s is not inside the state directory", logDir)
	}
	return strings.Split(rel, string(filepath.Separator)), nil
}
