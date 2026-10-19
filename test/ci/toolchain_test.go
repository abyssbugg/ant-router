package ci_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// Every CI system installs the go.mod toolchain. Go releases before 1.27.0
// can fail a healthy -fuzztime run with a bare "context deadline exceeded"
// (golang/go#75804, not backported to 1.26.x), which made the fuzz smoke gate
// fail on unrelated changes.
func TestToolchainIncludesFuzztimeDeadlineFix(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	match := regexp.MustCompile(`(?m)^toolchain go(\d+)\.(\d+)\.(\d+)\s*$`).FindSubmatch(data)
	if match == nil {
		t.Fatal("go.mod must pin an exact release toolchain (toolchain goX.Y.Z)")
	}
	var version [3]int
	for i := range version {
		if version[i], err = strconv.Atoi(string(match[i+1])); err != nil {
			t.Fatalf("parse toolchain version: %v", err)
		}
	}
	if version[0] < 1 || (version[0] == 1 && version[1] < 27) {
		t.Fatalf("toolchain go%d.%d.%d lacks the golang/go#75804 fix; need go1.27.0 or later", version[0], version[1], version[2])
	}
}
