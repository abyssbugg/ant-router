package ci_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// This contract covers the safety controls that must survive CI configuration edits.
// CircleCI config validate/process additionally check the provider's current schema.
func TestWindowsDiagnosticsRequireExplicitApproval(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".circleci", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Version    string `yaml:"version"`
		Parameters map[string]struct {
			Type    string `yaml:"type"`
			Default *bool  `yaml:"default"`
		} `yaml:"parameters"`
		Jobs map[string]struct {
			ResourceClass string `yaml:"resource_class"`
			Machine       struct {
				Image string `yaml:"image"`
			} `yaml:"machine"`
			Shell string      `yaml:"shell"`
			Steps []yaml.Node `yaml:"steps"`
		} `yaml:"jobs"`
		Workflows map[string]struct {
			When yaml.Node `yaml:"when"`
			Jobs []map[string]struct {
				Type     string   `yaml:"type"`
				Requires []string `yaml:"requires"`
			} `yaml:"jobs"`
		} `yaml:"workflows"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != "2.1" {
		t.Fatalf("unexpected CircleCI version %q", config.Version)
	}
	parameter, ok := config.Parameters["run-windows-diagnostics"]
	if !ok || len(config.Parameters) != 2 || parameter.Type != "boolean" || parameter.Default == nil || *parameter.Default {
		t.Fatal("Windows execution must require an explicitly enabled Boolean parameter")
	}
	workflow, ok := config.Workflows["windows-diagnostics"]
	if !ok || len(config.Workflows) != 2 {
		t.Fatal("expected only diagnostic and coverage workflows")
	}
	if err := validateCoverageConfig(data); err != nil {
		t.Fatal(err)
	}
	if len(workflow.Jobs) != 2 || len(workflow.Jobs[0]) != 1 || len(workflow.Jobs[1]) != 1 {
		t.Fatal("expected only an approval gate and one diagnostic job")
	}
	approval, ok := workflow.Jobs[0]["approve-windows-credits"]
	if !ok || approval.Type != "approval" {
		t.Fatal("the first workflow job must hold execution for approval")
	}
	invocation, ok := workflow.Jobs[1]["windows-token-diagnostics"]
	if !ok || len(invocation.Requires) != 1 || invocation.Requires[0] != "approve-windows-credits" {
		t.Fatal("the Windows job must depend on credit-use approval")
	}
	job, ok := config.Jobs["windows-token-diagnostics"]
	if !ok || len(config.Jobs) != 3 || job.ResourceClass != "windows.medium" || job.Machine.Image != "windows-server-2022-gui:current" {
		t.Fatal("expected exactly one medium native Windows VM")
	}
	if job.Shell != "powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass" {
		t.Fatal("expected an explicit noninteractive PowerShell shell")
	}
	if len(job.Steps) != 2 || job.Steps[0].Value != "checkout" {
		t.Fatal("expected only checkout and the bounded diagnostic script")
	}
	var step struct {
		Run struct {
			NoOutputTimeout string `yaml:"no_output_timeout"`
			Command         string `yaml:"command"`
		} `yaml:"run"`
	}
	if err := job.Steps[1].Decode(&step); err != nil {
		t.Fatal(err)
	}
	if step.Run.NoOutputTimeout != "5m" {
		t.Fatal("diagnostics must have a bounded no-output timeout")
	}
	script := step.Run.Command
	if err := validateWindowsStatements(script); err != nil {
		t.Error(err)
	}
}

// Compare the entire cost-bearing configuration, including unknown keys, so
// contexts, extra jobs, parallelism, schedules and retry settings cannot slip in.
func validateCoverageConfig(data []byte) error {
	var got, want map[string]any
	if err := yaml.Unmarshal(data, &got); err != nil {
		return err
	}
	if err := yaml.Unmarshal([]byte(coverageContract), &want); err != nil {
		return err
	}
	jobs, ok := got["jobs"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing jobs")
	}
	diagnostic, ok := jobs["windows-token-diagnostics"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing diagnostic job")
	}
	steps, ok := diagnostic["steps"].([]any)
	if !ok || len(steps) != 2 {
		return fmt.Errorf("unexpected diagnostic steps")
	}
	runStep, ok := steps[1].(map[string]any)
	if !ok {
		return fmt.Errorf("missing diagnostic run step")
	}
	run, ok := runStep["run"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing diagnostic command")
	}
	// Its executable body is independently checked by the existing mutation suite.
	run["command"] = "DIAGNOSTIC_BODY"
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("CI configuration differs from guarded coverage contract")
	}
	return nil
}

const coverageContract = `version: 2.1
parameters:
  run-windows-diagnostics: {type: boolean, default: false}
  run-pr-coverage: {type: boolean, default: false}
jobs:
  windows-token-diagnostics:
    machine: {image: "windows-server-2022-gui:current"}
    resource_class: windows.medium
    shell: powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass
    steps:
      - checkout
      - run:
          name: Verify native Windows and characterize token sharing
          no_output_timeout: 5m
          command: DIAGNOSTIC_BODY
  pr-coverage-linux:
    docker: [{image: "cimg/go:1.26.6"}]
    resource_class: medium
    shell: /bin/bash -eo pipefail
    steps:
      - checkout
      - run:
          name: Bounded Linux PR coverage
          no_output_timeout: 5m
          command: bash .circleci/scripts/pr-coverage-linux.sh
  pr-coverage-windows:
    machine: {image: "windows-server-2022-gui:current"}
    resource_class: windows.medium
    shell: powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass
    steps:
      - checkout
      - run:
          name: Bounded Windows PR coverage
          no_output_timeout: 5m
          command: powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File .circleci/scripts/pr-coverage-windows.ps1
workflows:
  windows-diagnostics:
    when:
      and:
        - << pipeline.parameters.run-windows-diagnostics >>
        - not: << pipeline.parameters.run-pr-coverage >>
    jobs:
      - approve-windows-credits: {type: approval}
      - windows-token-diagnostics: {requires: [approve-windows-credits]}
  pr-coverage:
    when:
      and:
        - << pipeline.parameters.run-pr-coverage >>
        - not: << pipeline.parameters.run-windows-diagnostics >>
    jobs:
      - approve-linux-coverage: {type: approval}
      - pr-coverage-linux: {requires: [approve-linux-coverage]}
      - approve-windows-coverage: {type: approval, requires: [pr-coverage-linux]}
      - pr-coverage-windows: {requires: [approve-windows-coverage]}
`

func TestCoverageContractMutations(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".circleci", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCoverageConfig(data); err != nil {
		t.Fatal(err)
	}
	for name, mutation := range map[string][2]string{
		"enabled default":         {"default: false", "default: true"},
		"removed exclusion":       {"not: << pipeline.parameters.run-pr-coverage >>", "not: false"},
		"parallel windows":        {"- pr-coverage-linux\n", "- approve-linux-coverage\n"},
		"bypass windows approval": {"- approve-windows-coverage\n", "- pr-coverage-linux\n"},
		"larger resource":         {"resource_class: medium", "resource_class: large"},
		"parallelism":             {"resource_class: medium", "parallelism: 2\n    resource_class: medium"},
		"mutable image":           {"cimg/go:1.26.6", "cimg/go:latest"},
		"unbounded command":       {"bash .circleci/scripts/pr-coverage-linux.sh", "make ci"},
		"context":                 {"type: approval", "type: approval\n          context: secrets"},
	} {
		t.Run(name, func(t *testing.T) {
			changed := strings.Replace(string(data), mutation[0], mutation[1], 1)
			if changed == string(data) {
				t.Fatal("mutation did not apply")
			}
			if validateCoverageConfig([]byte(changed)) == nil {
				t.Fatal("unsafe mutation accepted")
			}
		})
	}
}

var offlineTests = []string{"TestConfigurationWorkflow", "TestMockResponsesSubprocessWorkflow", "TestAnthropicProviderSubprocessAvailability"}

func TestCoverageResultEvidence(t *testing.T) {
	// Run the actual script parser, not a duplicate Go implementation of it.
	for _, platform := range []string{"linux", "windows"} {
		t.Run(platform, func(t *testing.T) {
			script := filepath.Join("..", "..", ".circleci", "scripts", "pr-coverage-"+platform+map[string]string{"linux": ".sh", "windows": ".ps1"}[platform])
			if _, err := os.Stat(script); err != nil {
				t.Fatal(err)
			}
			var executable string
			var prefix []string
			if platform == "windows" {
				if runtime.GOOS != "windows" {
					t.Skip("native PowerShell parser exercised by future Windows coverage")
				}
				executable = "powershell.exe"
				prefix = []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-VerifyResults"}
			} else {
				if runtime.GOOS == "windows" {
					t.Skip("Linux parser exercised on Unix hosts")
				}
				executable = "bash"
				prefix = []string{script, "--verify-results"}
			}
			for _, mode := range []string{"pass", "missing", "skip", "fail", "wrong-package", "package-fail", "package-missing", "malformed"} {
				t.Run(mode, func(t *testing.T) {
					var events bytes.Buffer
					enc := json.NewEncoder(&events)
					for i, name := range offlineTests {
						if mode == "missing" && i == 0 {
							continue
						}
						action, pkg := "pass", "github.com/abyssbugg/ant-router/test/e2e"
						if i == 0 && (mode == "skip" || mode == "fail") {
							action = mode
						}
						if mode == "wrong-package" {
							pkg = "unrelated/package"
						}
						if err := enc.Encode(map[string]string{"Action": action, "Test": name, "Package": pkg}); err != nil {
							t.Fatal(err)
						}
					}
					if mode != "package-missing" {
						action := "pass"
						if mode == "package-fail" {
							action = "fail"
						}
						if err := enc.Encode(map[string]string{"Action": action, "Package": "github.com/abyssbugg/ant-router/test/e2e"}); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "malformed" {
						events.WriteString("{not-json}\n")
					}
					file := filepath.Join(t.TempDir(), "events.json")
					if err := os.WriteFile(file, events.Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
					args := append(append([]string{}, prefix...), file)
					out, err := exec.Command(executable, args...).CombinedOutput()
					if (err == nil) != (mode == "pass") {
						t.Fatalf("mode=%s err=%v output=%s", mode, err, out)
					}
				})
			}
		})
	}
}

var coverageSafeguards = map[string][]string{
	"linux": {
		"set -euo pipefail",
		"exec timeout --signal=TERM --kill-after=5s 720s bash \"$0\" --worker",
		"[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]]",
		"[[ -n ${CIRCLE_SHA1:-} && $revision == \"$CIRCLE_SHA1\" ]]",
		"export PATH=\"$goroot/bin:$PATH\" GOTOOLCHAIN=local",
		"[[ $(go version) == \"go version $toolchain linux/amd64\" ]]",
		"command -v gcc >/dev/null",
		"go vet ./...",
		"go test ./... -count=1 -timeout=5m",
		"go test -race ./... -count=1 -timeout=5m",
		"go build ./cmd/routerd ./cmd/routerctl",
		"make cross-build",
		"verify_results \"$results\"",
	},
	"windows": {
		"$ErrorActionPreference = 'Stop'",
		"if (-not $child.WaitForExit(720000)) {",
		"& taskkill.exe /PID $child.Id /T /F",
		"exit $child.ExitCode",
		"if ($env:OS -ne 'Windows_NT') { throw 'Native Windows required' }",
		"if (-not $env:CIRCLE_SHA1 -or $revision -ne $env:CIRCLE_SHA1) { throw 'Checkout revision mismatch' }",
		"$env:PATH = (Join-Path $goroot 'bin') + ';' + $env:PATH",
		"$env:GOTOOLCHAIN = 'local'",
		"if ($version -ne \"go version $toolchain windows/amd64\") { throw 'Pinned compiler mismatch' }",
		"$null = Get-Command choco -ErrorAction Stop",
		"choco upgrade mingw --version=13.2.0 --source=https://community.chocolatey.org/api/v2/ --yes --no-progress --require-checksums --allow-downgrade --install-if-not-installed --execution-timeout=180",
		"$mingwBin = 'C:\\ProgramData\\mingw64\\mingw64\\bin'",
		"if (-not (Test-Path -LiteralPath (Join-Path $mingwBin 'gcc.exe') -PathType Leaf)) { throw 'Pinned MinGW compiler missing after installation' }",
		"$env:PATH = $mingwBin + ';' + $env:PATH",
		"$null = Get-Command gcc -ErrorAction Stop",
		"$gccVersion = gcc -dumpfullversion",
		"if ($gccVersion -ne '13.2.0') { throw 'Pinned MinGW version mismatch' }",
		"$gccTarget = gcc -dumpmachine",
		"if ($gccTarget -ne 'x86_64-w64-mingw32') { throw 'MinGW target must be Windows amd64' }",
		"$raceLibrary = gcc -print-file-name=libsynchronization.a",
		"if (-not [System.IO.Path]::IsPathRooted($raceLibrary) -or -not (Test-Path -LiteralPath $raceLibrary -PathType Leaf)) { throw 'MinGW runtime lacks Go race support' }",
		"go vet ./...",
		"go test ./... -count=1 -timeout=5m",
		"go test -race ./internal/controltoken ./internal/inferencetoken -count=1 -timeout=3m",
		"go test ./internal/controltransport -run '^TestWindows' -count=20 -timeout=3m",
		"go build ./cmd/routerd ./cmd/routerctl",
		"Assert-Results $results",
	},
}

func validateCoverageScript(platform, script string) error {
	lines := strings.Split(script, "\n")
	next := 0
	for _, required := range coverageSafeguards[platform] {
		found := false
		for next < len(lines) {
			line := strings.TrimSpace(lines[next])
			next++
			if line == required {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("missing or out-of-order %s safeguard: %s", platform, required)
		}
	}
	if platform == "windows" {
		return validateNativeExits(script)
	}
	return nil
}

func TestCoverageScriptSafeguards(t *testing.T) {
	for platform := range coverageSafeguards {
		ext := ".sh"
		if platform == "windows" {
			ext = ".ps1"
		}
		data, err := os.ReadFile(filepath.Join("..", "..", ".circleci", "scripts", "pr-coverage-"+platform+ext))
		if err != nil {
			t.Fatal(err)
		}
		script := string(data)
		if err := validateCoverageScript(platform, script); err != nil {
			t.Fatal(err)
		}
		for i, required := range coverageSafeguards[platform] {
			t.Run(fmt.Sprintf("%s/removed-%d", platform, i), func(t *testing.T) {
				mutated := strings.Replace(script, required, "# "+required, 1)
				if validateCoverageScript(platform, mutated) == nil {
					t.Fatal("commented safeguard accepted")
				}
			})
		}
	}
}

var requiredWindowsStatements = []string{
	"$ErrorActionPreference = 'Stop'",
	"$revision = git rev-parse HEAD",
	"if (-not $env:CIRCLE_SHA1 -or $revision -ne $env:CIRCLE_SHA1) {",
	"$module = Get-Content go.mod",
	"$env:GOTOOLCHAIN = $toolchain",
	"$version = go version",
	"$hostOS = go env GOHOSTOS",
	"$hostArch = go env GOHOSTARCH",
	"if ($hostOS -ne 'windows' -or $hostArch -ne 'amd64') {",
	"go test ./internal/controltoken ./internal/inferencetoken -run '^TestWindows' -count=10 -timeout=3m -v",
	"go test ./internal/controltoken ./internal/inferencetoken -run '^TestConcurrentLoadOrCreateConverges$' -count=20 -timeout=3m -v",
	"go test ./internal/controltoken ./internal/inferencetoken -count=1 -timeout=3m",
}

func validateWindowsStatements(script string) error {
	// This is a regression guard for the deliberately simple, line-oriented
	// script, not a PowerShell parser. Reject multiline constructs rather than
	// accidentally count statements inside block comments or here-strings.
	for _, delimiter := range []string{"<#", "#>", "@\"", "@'"} {
		if strings.Contains(script, delimiter) {
			return fmt.Errorf("unsupported multiline PowerShell construct %q", delimiter)
		}
	}
	lines := strings.Split(script, "\n")
	next := 0
	for _, required := range requiredWindowsStatements {
		found := -1
		for i := next; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == required {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Errorf("missing or out-of-order executable diagnostic safeguard %q", required)
		}
		next = found + 1
	}
	return validateNativeExits(script)
}

const nativeExitCheck = "if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }"

func validateNativeExits(script string) error {
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") || strings.HasPrefix(line, "git ") || strings.HasPrefix(line, "choco ") || strings.HasPrefix(line, "gcc ") || strings.Contains(line, " = go ") || strings.Contains(line, " = git ") || strings.Contains(line, " = gcc ") {
			if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != nativeExitCheck {
				return fmt.Errorf("native command lacks immediate failure propagation: %s", line)
			}
		}
	}
	return nil
}

func TestWindowsStatementMutations(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".circleci", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// Decode the checkout scalar separately from the run mapping.
	var raw struct {
		Jobs map[string]struct {
			Steps []yaml.Node `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	steps := raw.Jobs["windows-token-diagnostics"].Steps
	if len(steps) != 2 {
		t.Fatal("expected two job steps")
	}
	var step struct {
		Run struct {
			Command string `yaml:"command"`
		} `yaml:"run"`
	}
	if err := steps[1].Decode(&step); err != nil {
		t.Fatal(err)
	}
	script := step.Run.Command
	if err := validateWindowsStatements(script); err != nil {
		t.Fatal(err)
	}
	for i, statement := range requiredWindowsStatements {
		for name, replacement := range map[string]string{
			"removed":         "",
			"commented":       "# " + statement,
			"output-only":     "Write-Host '" + strings.ReplaceAll(statement, "'", "''") + "'",
			"block-commented": "<#\n" + statement + "\n#>",
		} {
			t.Run(fmt.Sprintf("statement-%d/%s", i, name), func(t *testing.T) {
				mutated := strings.Replace(script, statement, replacement, 1)
				if mutated == script {
					t.Fatal("mutation did not change the script")
				}
				if err := validateWindowsStatements(mutated); err == nil {
					t.Fatalf("accepted %s safeguard: %s", name, statement)
				}
			})
		}
	}
	for i, statement := range requiredWindowsStatements[:len(requiredWindowsStatements)-1] {
		t.Run(fmt.Sprintf("statement-%d/moved-after-dependent-command", i), func(t *testing.T) {
			block := statement + "\n"
			if strings.Contains(script, block+nativeExitCheck+"\n") {
				block += nativeExitCheck + "\n"
			}
			mutated := strings.Replace(script, block, "", 1) + "\n" + block
			if mutated == script {
				t.Fatal("mutation did not change the script")
			}
			if err := validateWindowsStatements(mutated); err == nil {
				t.Fatalf("accepted out-of-order safeguard: %s", statement)
			}
		})
	}
	lines := strings.Split(script, "\n")
	checks := 0
	for i, line := range lines {
		if strings.TrimSpace(line) != nativeExitCheck {
			continue
		}
		checks++
		for name, replacement := range map[string]string{
			"removed":     "",
			"commented":   "# " + nativeExitCheck,
			"output-only": "Write-Host '" + nativeExitCheck + "'",
		} {
			t.Run(fmt.Sprintf("exit-check-%d/%s", i, name), func(t *testing.T) {
				mutated := append([]string(nil), lines...)
				mutated[i] = replacement
				if err := validateWindowsStatements(strings.Join(mutated, "\n")); err == nil {
					t.Fatalf("accepted %s exit-code check at line %d", name, i+1)
				}
			})
		}
	}
	if checks == 0 {
		t.Fatal("expected native exit-code checks to mutate")
	}
}
