package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

var toolchainDirective = regexp.MustCompile(`(?m)^toolchain go(\d+)\.(\d+)\.(\d+)\s*$`)

// toolchainAtLeast reports whether go.mod names an exact release toolchain
// (toolchain goX.Y.Z) at or above major.minor.0. CI installs exactly this
// release, so prerelease, "default" or absent directives are rejected.
func toolchainAtLeast(goMod []byte, major, minor int) (bool, error) {
	match := toolchainDirective.FindSubmatch(goMod)
	if match == nil {
		return false, fmt.Errorf("go.mod must name an exact release toolchain (toolchain goX.Y.Z)")
	}
	var version [3]int
	for i := range version {
		value, err := strconv.Atoi(string(match[i+1]))
		if err != nil {
			return false, fmt.Errorf("parse toolchain version: %w", err)
		}
		version[i] = value
	}
	return version[0] > major || (version[0] == major && version[1] >= minor), nil
}

func TestToolchainAtLeastBoundaries(t *testing.T) {
	for _, tc := range []struct {
		goMod   string
		want    bool
		wantErr bool
	}{
		{"go 1.25.0\n\ntoolchain go1.26.6\n", false, false},
		{"go 1.25.0\n\ntoolchain go1.26.99\n", false, false},
		{"go 1.25.0\n\ntoolchain go1.27.0\n", true, false},
		{"go 1.25.0\n\ntoolchain go1.27.2\n", true, false},
		{"go 1.25.0\n\ntoolchain go1.28.0\n", true, false},
		{"go 1.25.0\n\ntoolchain go2.0.0\n", true, false},
		{"go 1.27.2\n", false, true},
		{"go 1.25.0\n\ntoolchain go1.27.0rc1\n", false, true},
		{"go 1.25.0\n\ntoolchain default\n", false, true},
	} {
		got, err := toolchainAtLeast([]byte(tc.goMod), 1, 27)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("toolchainAtLeast(%q) = %v, %v; want %v, error %v", tc.goMod, got, err, tc.want, tc.wantErr)
		}
	}
}

// Every CI system installs the go.mod toolchain. Go releases before 1.27.0
// can fail a healthy -fuzztime run with a bare "context deadline exceeded"
// (golang/go#75804, not backported to 1.26.x), which made the fuzz smoke gate
// fail on unrelated changes.
func TestToolchainIncludesFuzztimeDeadlineFix(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	ok, err := toolchainAtLeast(data, 1, 27)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("go.mod toolchain must be at least go1.27.0 (golang/go#75804)")
	}
}
