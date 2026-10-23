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
// normally exits 0. STUB_MODES lists "tool:mode" pairs selecting how T behaves,
// covering each way the real CLI can signal an analyzer failure:
//   - empty-runs:  the "runs": [] document written for a failed/unknown tool
//   - exec-failed: a run whose invocation reports "executionSuccessful": false
//   - no-output:   no SARIF file at all
//   - empty-file:  a zero-length SARIF file
//   - exit-status: a well-formed successful SARIF run but a nonzero CLI exit
//   - wrong-driver: a successful run, but of a different analyzer
//
// and one success mode:
//   - findings:    a compact single-line SARIF run with three findings
//
// Any other tool gets a successful run with no findings.
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
mode=ok
for f in $STUB_MODES; do
  case "$f" in
    "$tool":*) mode="${f#*:}" ;;
  esac
done
case "$mode" in
  empty-runs) printf '{"runs": [], "version": "2.1.0"}\n' > "$out" ;;
  exec-failed) printf '{"runs": [{"tool": {"driver": {"name": "%s"}}, "invocations": [{"executionSuccessful": false}], "results": []}], "version": "2.1.0"}\n' "$tool" > "$out" ;;
  no-output) ;;
  empty-file) : > "$out" ;;
  exit-status) printf '{"runs": [{"tool": {"driver": {"name": "%s"}}, "invocations": [{"executionSuccessful": true}], "results": []}], "version": "2.1.0"}\n' "$tool" > "$out"; exit 3 ;;
  wrong-driver) printf '{"runs": [{"tool": {"driver": {"name": "Other", "rules": [{"name": "%s"}]}}, "invocations": [{"executionSuccessful": true}], "results": []}], "version": "2.1.0"}\n' "$tool" > "$out" ;;
  findings) printf '{"runs":[{"tool":{"driver":{"name":"%s"}},"invocations":[{"executionSuccessful":true}],"results":[{"ruleId":"a"},{"ruleId":"b"},{"ruleId":"c"}]}],"version":"2.1.0"}\n' "$tool" > "$out" ;;
  *) printf '{"runs": [{"tool": {"driver": {"name": "%s"}}, "invocations": [{"executionSuccessful": true}], "results": []}], "version": "2.1.0"}\n' "$tool" > "$out" ;;
esac
exit 0
`

func runCodacyAnalyze(t *testing.T, modes string) (string, []string, error) {
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
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_LOG="+log, "STUB_MODES="+modes)
	output, runErr := cmd.CombinedOutput()
	data, _ := os.ReadFile(log)
	return string(output), strings.Fields(string(data)), runErr
}

var configuredTools = []string{"lizard", "opengrep", "revive", "trivy"}

// Every tool configured in .codacy/codacy.yaml must run; the target must not
// drift from the configuration.
func TestCodacyAnalyzeRunsEveryConfiguredTool(t *testing.T) {
	output, calls, err := runCodacyAnalyze(t, "")
	if err != nil {
		t.Fatalf("codacy-analyze.sh failed: %v\n%s", err, output)
	}
	if strings.Join(calls, " ") != strings.Join(configuredTools, " ") {
		t.Fatalf("tools run = %v, want %v", calls, configuredTools)
	}
}

// codacy-cli exits 0 when an analyzer fails; the script must turn every SARIF
// failure signal into a failure that names the tool, while still running each
// configured tool exactly once so all failures are reported.
func TestCodacyAnalyzeFailsWhenAnAnalyzerFails(t *testing.T) {
	for _, mode := range []string{"empty-runs", "exec-failed", "no-output", "empty-file", "exit-status", "wrong-driver"} {
		t.Run(mode, func(t *testing.T) {
			output, calls, err := runCodacyAnalyze(t, "opengrep:"+mode)
			if err == nil {
				t.Fatalf("codacy-analyze.sh succeeded although opengrep failed (%s):\n%s", mode, output)
			}
			if got := lastLine(output); got != "codacy-analyze: failed analyzers: opengrep" {
				t.Fatalf("failure summary = %q, want exactly opengrep:\n%s", got, output)
			}
			if strings.Join(calls, " ") != strings.Join(configuredTools, " ") {
				t.Fatalf("tools run = %v, want %v", calls, configuredTools)
			}
		})
	}
}

// The finding count must count findings, not SARIF lines: codacy-cli writes
// compact single-line SARIF.
func TestCodacyAnalyzeCountsEveryFinding(t *testing.T) {
	output, _, err := runCodacyAnalyze(t, "lizard:findings")
	if err != nil {
		t.Fatalf("codacy-analyze.sh failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "\nlizard: 3 finding(s)\n") {
		t.Fatalf("output does not report 3 lizard findings:\n%s", output)
	}
}

func lastLine(output string) string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	return lines[len(lines)-1]
}
