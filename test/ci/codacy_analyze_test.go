package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubCodacyCLI mimics codacy-cli v2: "analyze --tool T --format sarif -o F"
// always exits 0, writing a SARIF run for T unless T is listed in
// STUB_FAIL_TOOLS, in which case it writes the empty "runs": [] document the
// real CLI produces when an analyzer fails or is unknown.
const stubCodacyCLI = `#!/bin/sh
tool="" out=""
while [ $# -gt 0 ]; do
  case "$1" in
    --tool) tool="$2"; shift ;;
    -o) out="$2"; shift ;;
  esac
  shift
done
echo "$tool" >> "$STUB_LOG"
case " $STUB_FAIL_TOOLS " in
  *" $tool "*) printf '{"runs": [], "version": "2.1.0"}\n' > "$out" ;;
  *) printf '{"runs": [{"tool": {"driver": {"name": "%s"}}, "results": []}], "version": "2.1.0"}\n' "$tool" > "$out" ;;
esac
exit 0
`

func runCodacyAnalyze(t *testing.T, failTools string) (string, []string, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("codacy-analyze.sh is a POSIX shell script")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codacy-cli"), []byte(stubCodacyCLI), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls.log")
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "codacy-analyze.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_LOG="+log, "STUB_FAIL_TOOLS="+failTools)
	output, runErr := cmd.CombinedOutput()
	data, _ := os.ReadFile(log)
	return string(output), strings.Fields(string(data)), runErr
}

// Every tool configured in .codacy/codacy.yaml must run; the target must not
// drift from the configuration.
func TestCodacyAnalyzeRunsEveryConfiguredTool(t *testing.T) {
	output, calls, err := runCodacyAnalyze(t, "")
	if err != nil {
		t.Fatalf("codacy-analyze.sh failed: %v\n%s", err, output)
	}
	want := []string{"lizard", "opengrep", "revive", "trivy"}
	if strings.Join(calls, " ") != strings.Join(want, " ") {
		t.Fatalf("tools run = %v, want %v", calls, want)
	}
}

// codacy-cli exits 0 when an analyzer fails; the script must turn the empty
// SARIF result into a failure that names the tool.
func TestCodacyAnalyzeFailsWhenAnAnalyzerProducesNoRun(t *testing.T) {
	output, calls, err := runCodacyAnalyze(t, "opengrep")
	if err == nil {
		t.Fatalf("codacy-analyze.sh succeeded although opengrep produced no run:\n%s", output)
	}
	if !strings.Contains(output, "opengrep") {
		t.Fatalf("failure output does not name the failed tool:\n%s", output)
	}
	if len(calls) != 4 {
		t.Fatalf("tools run = %v; every tool should still run so all failures are reported", calls)
	}
}
