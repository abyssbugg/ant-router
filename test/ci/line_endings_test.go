package ci

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckoutLineEndingsAreLF guards Windows CI: without an explicit eol
// attribute, Git for Windows (core.autocrlf=true) checks text files out with
// CRLF, which breaks line-anchored test assertions, go.mod parsing and shell
// scripts. Every checkout must resolve text files to eol=lf.
func TestCheckoutLineEndingsAreLF(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"go.mod",
		"scripts/install.sh",
		"scripts/install.ps1",
		".circleci/scripts/windows.ps1",
		".goreleaser.yaml",
		"internal/canonical/request.go",
	}
	args := append([]string{"-C", root, "check-attr", "text", "eol", "--"}, paths...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git check-attr: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, ": ", 3)
		if len(parts) != 3 {
			t.Fatalf("unexpected git check-attr line %q", line)
		}
		got[parts[0]+" "+parts[1]] = strings.TrimSpace(parts[2])
	}
	for _, p := range paths {
		if v := got[p+" eol"]; v != "lf" {
			t.Errorf("%s: eol = %q, want lf", p, v)
		}
		if v := got[p+" text"]; v == "unset" || v == "unspecified" {
			t.Errorf("%s: text = %q, want text normalization enabled", p, v)
		}
	}
}
