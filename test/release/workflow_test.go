package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestReleaseWorkflowTrigger verifies that the workflow triggers only on push to version tags.
func TestReleaseWorkflowTrigger(t *testing.T) {
	workflow := loadReleaseWorkflow(t)

	// Check on.push.tags exists and only has v* pattern
	on := field(t, workflow, "on")
	push := field(t, on, "push")
	tags := list(t, push, "tags")

	if len(tags) != 1 {
		t.Fatalf("expected exactly 1 tag pattern, got %d", len(tags))
	}

	tagPattern := tags[0].(string)
	if tagPattern != "v*" {
		t.Fatalf("expected tag pattern 'v*', got %q", tagPattern)
	}

	// Verify no pull_request, workflow_dispatch, pull_request_target
	on = field(t, workflow, "on")
	if _, ok := on.(map[string]any)["pull_request"]; ok {
		t.Fatal("workflow should not have pull_request trigger")
	}
	if _, ok := on.(map[string]any)["workflow_dispatch"]; ok {
		t.Fatal("workflow should not have workflow_dispatch trigger")
	}
	if _, ok := on.(map[string]any)["pull_request_target"]; ok {
		t.Fatal("workflow should not have pull_request_target trigger")
	}
}

// TestReleaseWorkflowTopLevelPermissionsEmpty verifies the workflow denies
// all token permissions by default. A bare `permissions:` key is YAML null,
// which GitHub treats as "not set" (default token permissions), so the value
// must be the explicit empty mapping `{}`.
func TestReleaseWorkflowTopLevelPermissionsEmpty(t *testing.T) {
	workflow := loadReleaseWorkflow(t)
	perms, ok := workflow["permissions"]
	if !ok {
		t.Fatal("top-level permissions must be declared")
	}
	if perms != emptyFlowMapping {
		t.Fatalf("top-level permissions must be {} (deny all), got %T %v", perms, perms)
	}
}

// TestReleaseWorkflowJobPermissions verifies job has exactly contents:write and id-token:write.
func TestReleaseWorkflowJobPermissions(t *testing.T) {
	workflow := loadReleaseWorkflow(t)

	jobs := field(t, workflow, "jobs")
	releaseJob := field(t, jobs, "release")
	perms := field(t, releaseJob, "permissions")

	permMap, ok := perms.(map[string]any)
	if !ok {
		t.Fatal("job permissions not a map")
	}

	if len(permMap) != 2 {
		t.Fatalf("expected exactly 2 permissions, got %d", len(permMap))
	}

	contentsVal, hasContents := permMap["contents"]
	if !hasContents {
		t.Fatal("missing 'contents' permission")
	}
	if contentsVal != "write" {
		t.Fatalf("expected contents: write, got %v", contentsVal)
	}

	idTokenVal, hasIDToken := permMap["id-token"]
	if !hasIDToken {
		t.Fatal("missing 'id-token' permission")
	}
	if idTokenVal != "write" {
		t.Fatalf("expected id-token: write, got %v", idTokenVal)
	}
}

// TestReleaseWorkflowUsesArePinned verifies all 'uses:' steps are pinned to 40-hex SHAs with version comments.
func TestReleaseWorkflowUsesArePinned(t *testing.T) {
	data, err := os.ReadFile(workflowPath(t))
	if err != nil {
		t.Fatalf("failed to read workflow file: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	// Match lines like "      - uses: owner/repo@<40-hex> # v1.2.3"
	pinRx := regexp.MustCompile(`^\s*-\s+uses:\s+([a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(?:/[a-zA-Z0-9_.-]+)*)@([0-9a-f]{40})\s+#\s*v\d+\.\d+\.\d+\s*$`)

	foundUses := 0
	for i, line := range lines {
		if !strings.Contains(line, "uses:") {
			continue
		}

		match := pinRx.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("line %d: 'uses:' not properly pinned or formatted: %s", i+1, line)
		}

		foundUses++
	}

	// We expect at least 4 uses: (checkout, setup-go, cosign-installer, goreleaser-action)
	if foundUses < 4 {
		t.Fatalf("expected at least 4 pinned actions, found %d", foundUses)
	}
}

// TestReleaseWorkflowCheckoutConfig verifies checkout step has fetch-depth: 0 and persist-credentials: false.
func TestReleaseWorkflowCheckoutConfig(t *testing.T) {
	workflow := loadReleaseWorkflow(t)
	steps := getSteps(t, workflow)

	checkoutStep := findStepByUses(steps, "actions/checkout")
	if checkoutStep == nil {
		t.Fatal("checkout step not found")
	}

	with := field(t, checkoutStep, "with")

	// YAML parser may return string or int; compare as int
	fetchDepth := field(t, with, "fetch-depth")
	fd := toInt(fetchDepth)
	if fd != 0 {
		t.Fatalf("expected fetch-depth: 0, got %v (type %T)", fetchDepth, fetchDepth)
	}

	// YAML parser may return string or bool; compare as bool
	persistCreds := field(t, with, "persist-credentials")
	pc := toBool(persistCreds)
	if pc != false {
		t.Fatalf("expected persist-credentials: false, got %v (type %T)", persistCreds, persistCreds)
	}
}

// TestReleaseWorkflowSetupGoCache verifies setup-go has cache: false.
func TestReleaseWorkflowSetupGoCache(t *testing.T) {
	workflow := loadReleaseWorkflow(t)
	steps := getSteps(t, workflow)

	setupGoStep := findStepByUses(steps, "actions/setup-go")
	if setupGoStep == nil {
		t.Fatal("setup-go step not found")
	}

	with := field(t, setupGoStep, "with")
	cacheVal := field(t, with, "cache")
	c := toBool(cacheVal)
	if c != false {
		t.Fatalf("expected cache: false, got %v (type %T)", cacheVal, cacheVal)
	}
}

// TestReleaseWorkflowMakeCIPrecedes verifies 'make ci' step comes before goreleaser.
func TestReleaseWorkflowMakeCIPrecedes(t *testing.T) {
	workflow := loadReleaseWorkflow(t)
	steps := getSteps(t, workflow)

	var ciIdx, goreleaserIdx int
	ciIdx = -1
	goreleaserIdx = -1

	for i, step := range steps {
		stepMap := step.(map[string]any)

		if run, ok := stepMap["run"].(string); ok && strings.Contains(run, "make ci") {
			ciIdx = i
		}
		if uses, ok := stepMap["uses"].(string); ok && strings.Contains(uses, "goreleaser-action") {
			goreleaserIdx = i
		}
	}

	if ciIdx == -1 {
		t.Fatal("'make ci' step not found")
	}
	if goreleaserIdx == -1 {
		t.Fatal("goreleaser-action step not found")
	}

	if ciIdx >= goreleaserIdx {
		t.Fatalf("'make ci' step (index %d) should precede goreleaser (index %d)", ciIdx, goreleaserIdx)
	}
}

// TestReleaseWorkflowGoreleaserArgs verifies goreleaser-action receives
// `release --clean`. Action inputs are strings; a YAML list is not a valid
// input value.
func TestReleaseWorkflowGoreleaserArgs(t *testing.T) {
	workflow := loadReleaseWorkflow(t)
	steps := getSteps(t, workflow)
	goreleaserStep := findStepByUses(steps, "goreleaser-action")
	if goreleaserStep == nil {
		t.Fatal("goreleaser-action step not found")
	}
	raw := field(t, goreleaserStep, "with", "args")
	args, ok := raw.(string)
	if !ok {
		t.Fatalf("goreleaser args must be a string input, got %T %v", raw, raw)
	}
	if args != "release --clean" {
		t.Fatalf("goreleaser args = %q, want %q", args, "release --clean")
	}
}

// TestReleaseWorkflowNoUntrustedContext verifies no ${{ github.event.* }} in run: scripts.
func TestReleaseWorkflowNoUntrustedContext(t *testing.T) {
	workflow := loadReleaseWorkflow(t)
	steps := getSteps(t, workflow)

	untrustedPattern := regexp.MustCompile(`\$\{\{\s*github\.event\.`)

	for i, step := range steps {
		stepMap := step.(map[string]any)
		if run, ok := stepMap["run"].(string); ok {
			if untrustedPattern.MatchString(run) {
				t.Fatalf("step %d contains untrusted github.event context: %s", i, run)
			}
		}
	}
}

// TestReleaseWorkflowTapTokenEnvName verifies tap token env var matches .goreleaser.yaml reference.
func TestReleaseWorkflowTapTokenEnvName(t *testing.T) {
	// Read goreleaser config to find the env var name
	goreleaserPath := filepath.Join(repoRoot(t), ".goreleaser.yaml")
	goreleaserData, err := os.ReadFile(goreleaserPath)
	if err != nil {
		t.Fatalf("failed to read .goreleaser.yaml: %v", err)
	}

	// Simple line-based search for the token reference
	goreleaserText := string(goreleaserData)
	if !strings.Contains(goreleaserText, "HOMEBREW_TAP_GITHUB_TOKEN") {
		t.Fatal(".goreleaser.yaml does not reference HOMEBREW_TAP_GITHUB_TOKEN")
	}

	// Verify workflow uses the same env var in goreleaser-action step
	workflow := loadReleaseWorkflow(t)
	steps := getSteps(t, workflow)

	goreleaserStep := findStepByUses(steps, "goreleaser-action")
	if goreleaserStep == nil {
		t.Fatal("goreleaser-action step not found")
	}

	env := field(t, goreleaserStep, "env")
	if env == nil {
		t.Fatal("goreleaser step missing env")
	}

	envMap := env.(map[string]any)
	if _, ok := envMap["HOMEBREW_TAP_GITHUB_TOKEN"]; !ok {
		t.Fatal("goreleaser step missing HOMEBREW_TAP_GITHUB_TOKEN env var")
	}
}

// TestReleaseWorkflowFilenameMatchesInstallScript verifies workflow filename matches install.sh expectation.
func TestReleaseWorkflowFilenameMatchesInstallScript(t *testing.T) {
	// Read install.sh to verify it expects .github/workflows/release.yml
	installPath := filepath.Join(repoRoot(t), "scripts", "install.sh")
	installData, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatalf("failed to read install.sh: %v", err)
	}

	installText := string(installData)

	// Verify the expected identity in install.sh
	if !strings.Contains(installText, ".github/workflows/release.yml@refs/tags/") {
		t.Fatal("install.sh does not contain expected release.yml@refs/tags/ identity")
	}

	// Verify the workflow file actually exists at the expected path
	workflowPath := filepath.Join(repoRoot(t), ".github", "workflows", "release.yml")
	if _, err := os.Stat(workflowPath); err != nil {
		t.Fatalf("release.yml not found at expected path: %v", err)
	}
}

// Helper functions

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case string:
		// Try to parse as int
		i, err := strconv.Atoi(val)
		if err != nil {
			return 0
		}
		return i
	default:
		return 0
	}
}

func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "yes" || val == "on" || val == "1"
	default:
		return false
	}
}

func loadReleaseWorkflow(t *testing.T) map[string]any {
	data, err := os.ReadFile(workflowPath(t))
	if err != nil {
		t.Fatalf("failed to read release workflow: %v", err)
	}
	return parseYAML(t, data)
}

func workflowPath(t *testing.T) string {
	return filepath.Join(repoRoot(t), ".github", "workflows", "release.yml")
}

func getSteps(t *testing.T, workflow map[string]any) []any {
	jobs := field(t, workflow, "jobs")
	releaseJob := field(t, jobs, "release")
	return list(t, releaseJob, "steps")
}

func findStepByUses(steps []any, usesPartial string) map[string]any {
	for _, step := range steps {
		stepMap := step.(map[string]any)
		if uses, ok := stepMap["uses"].(string); ok {
			if strings.Contains(uses, usesPartial) {
				return stepMap
			}
		}
	}
	return nil
}

// TestReleaseWorkflowInstallsSyftForSBOMs verifies that when GoReleaser is
// configured to generate SBOMs (which shells out to syft), the workflow
// installs syft, puts it on PATH without interpolating step outputs into the
// script, and does so before GoReleaser runs.
func TestReleaseWorkflowInstallsSyftForSBOMs(t *testing.T) {
	cfg, err := os.ReadFile(filepath.Join(repoRoot(t), ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^sboms:`).Match(cfg) {
		t.Skip("goreleaser does not generate SBOMs")
	}
	steps := getSteps(t, loadReleaseWorkflow(t))
	syftIdx, pathIdx, goreleaserIdx := -1, -1, -1
	for i, raw := range steps {
		step, _ := raw.(map[string]any)
		uses, _ := step["uses"].(string)
		run, _ := step["run"].(string)
		switch {
		case strings.HasPrefix(uses, "anchore/sbom-action/download-syft@"):
			syftIdx = i
			if step["id"] != "syft" {
				t.Errorf("download-syft step must have id syft, got %v", step["id"])
			}
		case strings.Contains(run, "GITHUB_PATH") && strings.Contains(run, "SYFT_CMD"):
			pathIdx = i
			if strings.Contains(run, "${{") {
				t.Errorf("PATH step must not interpolate expressions into run: %q", run)
			}
			env, _ := step["env"].(map[string]any)
			if env["SYFT_CMD"] != "${{ steps.syft.outputs.cmd }}" {
				t.Errorf("PATH step env SYFT_CMD = %v", env["SYFT_CMD"])
			}
		case strings.Contains(uses, "goreleaser-action"):
			goreleaserIdx = i
		}
	}
	if syftIdx < 0 || pathIdx < 0 || goreleaserIdx < 0 {
		t.Fatalf("steps: download-syft=%d path=%d goreleaser=%d; all required", syftIdx, pathIdx, goreleaserIdx)
	}
	if !(syftIdx < pathIdx && pathIdx < goreleaserIdx) {
		t.Fatalf("order must be download-syft (%d) < PATH (%d) < goreleaser (%d)", syftIdx, pathIdx, goreleaserIdx)
	}
}
