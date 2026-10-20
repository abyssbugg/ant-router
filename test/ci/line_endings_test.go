package ci

import (
	"bytes"
	"os"
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

	// Behavioral check: write the indexed files out the way Git for Windows
	// does (core.autocrlf=true) and require LF-only bytes.
	prefix := t.TempDir() + string(filepath.Separator)
	co := append([]string{"-C", root, "-c", "core.autocrlf=true", "checkout-index", "--prefix=" + prefix, "--"}, paths...)
	if out, err := exec.Command("git", co...).CombinedOutput(); err != nil {
		t.Fatalf("git checkout-index: %v\n%s", err, out)
	}
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(prefix, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.IndexByte(data, '\r') >= 0 {
			t.Errorf("%s: checked out with a carriage return under core.autocrlf=true", p)
		}
	}
}
