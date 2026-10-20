package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestMakeCIMatchesBuildkiteQualityJob keeps local `make ci` honest: it must
// run exactly the Buildkite quality job's make targets, in order, plus the
// fuzz-smoke target.
func TestMakeCIMatchesBuildkiteQualityJob(t *testing.T) {
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	makeCI := regexp.MustCompile(`(?m)^ci:([^\n]*)$`).FindStringSubmatch(read("Makefile"))
	if makeCI == nil {
		t.Fatal("Makefile has no ci target")
	}
	quality := regexp.MustCompile(`(?s)\n  quality\)\n\s*for target in (.*?); do`).FindStringSubmatch(read(".buildkite/scripts/ci.sh"))
	if quality == nil {
		t.Fatal(".buildkite/scripts/ci.sh has no quality target loop")
	}
	want := append(strings.Fields(strings.ReplaceAll(quality[1], "\\\n", " ")), "fuzz-smoke")
	if got := strings.Fields(makeCI[1]); !slices.Equal(got, want) {
		t.Fatalf("make ci prerequisites = %v\nwant Buildkite quality targets + fuzz-smoke = %v", got, want)
	}
}
