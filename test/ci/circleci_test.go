package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
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
			When string `yaml:"when"`
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
	if !ok || len(config.Parameters) != 1 || parameter.Type != "boolean" || parameter.Default == nil || *parameter.Default {
		t.Fatal("Windows execution must require an explicitly enabled Boolean parameter")
	}
	workflow, ok := config.Workflows["windows-diagnostics"]
	if !ok || len(config.Workflows) != 1 || workflow.When != "<< pipeline.parameters.run-windows-diagnostics >>" {
		t.Fatal("all workflows must be gated by the disabled-by-default parameter")
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
	if !ok || len(config.Jobs) != 1 || job.ResourceClass != "windows.medium" || job.Machine.Image != "windows-server-2022-gui:current" {
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
		if strings.HasPrefix(line, "go ") || strings.HasPrefix(line, "git ") || strings.Contains(line, " = go ") || strings.Contains(line, " = git ") {
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
