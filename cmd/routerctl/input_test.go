package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/abyssbugg/ant-router/internal/config"
)

func TestReadConfigurationFromStdinAndFile(t *testing.T) {
	data := []byte("schema_version = 1\n")
	fromStdin, err := readConfiguration("-", bytes.NewReader(data))
	if err != nil || !bytes.Equal(fromStdin, data) {
		t.Fatalf("stdin = %q, %v", fromStdin, err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	fromFile, err := readConfiguration(path, bytes.NewReader(nil))
	if err != nil || !bytes.Equal(fromFile, data) {
		t.Fatalf("file = %q, %v", fromFile, err)
	}
}

func TestReadConfigurationRejectsOversizedInput(t *testing.T) {
	_, err := readConfiguration("-", bytes.NewReader(bytes.Repeat([]byte{'x'}, config.MaxSourceBytes+1)))
	if !errors.Is(err, errConfigurationTooLarge) {
		t.Fatalf("error = %v, want errConfigurationTooLarge", err)
	}
}
