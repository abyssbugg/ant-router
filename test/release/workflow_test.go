package release

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Releases are built, signed and published by CircleCI. The release config
// is read only by a dedicated pipeline definition whose sole trigger is
// "only-tags"; its URL is the subject of every keyless cosign certificate,
// so the installers verify exactly this identity. PR and branch builds use a
// different pipeline definition and therefore cannot mint a signature that
// the installers accept.
const (
	circleciProjectID          = "cc5f04e8-66e9-4f18-91b0-71c671674d14"
	releasePipelineDefinition  = "b37a0ec3-4a25-4686-b480-6b4eda16099f"
	releaseCertificateIdentity = "https://circleci.com/api/v2/projects/" + circleciProjectID + "/pipeline-definitions/" + releasePipelineDefinition
	releaseOIDCIssuer          = "https://oidc.circleci.com"
	releaseContext             = "ant-router-release"
	releaseTagFilter           = `/^v[0-9]+\.[0-9]+\.[0-9]+.*/`
)

func releaseConfigPath(t *testing.T) string {
	return filepath.Join(repoRoot(t), ".circleci", "release.yml")
}

func loadReleaseConfig(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(releaseConfigPath(t))
	if err != nil {
		t.Fatalf("read CircleCI release config: %v", err)
	}
	return parseYAML(t, data)
}

func mappingKeys(t *testing.T, node any, path ...string) []string {
	t.Helper()
	mapping, ok := field(t, node, path...).(map[string]any)
	if !ok {
		t.Fatalf("%s is not a mapping", strings.Join(path, "."))
	}
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// TestGitHubActionsReleaseWorkflowRemoved keeps a single signer: a second
// release workflow would sign with a different identity that the installers
// reject, and GitHub Actions cannot run for this repository.
func TestGitHubActionsReleaseWorkflowRemoved(t *testing.T) {
	path := filepath.Join(repoRoot(t), ".github", "workflows", "release.yml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s must not exist (err=%v); releases run on CircleCI", path, err)
	}
}

func TestCircleCIReleaseConfigRunsOnlyForVersionTags(t *testing.T) {
	config := loadReleaseConfig(t)
	if got := text(t, config, "version"); got != "2.1" {
		t.Fatalf("version = %q, want 2.1", got)
	}
	if got := mappingKeys(t, config); !slices.Equal(got, []string{"jobs", "version", "workflows"}) {
		t.Fatalf("top-level keys = %v, want exactly jobs, version, workflows", got)
	}
	if got := mappingKeys(t, config, "jobs"); !slices.Equal(got, []string{"release"}) {
		t.Fatalf("jobs = %v, want exactly release", got)
	}
	if got := mappingKeys(t, config, "workflows"); !slices.Equal(got, []string{"release"}) {
		t.Fatalf("workflows = %v, want exactly release", got)
	}
	jobs := list(t, config, "workflows", "release", "jobs")
	if len(jobs) != 2 {
		t.Fatalf("workflow jobs = %d, want the approval hold followed by release", len(jobs))
	}
	// A pushed tag alone never publishes: a maintainer must approve the
	// hold before the job that receives the publishing tokens can start.
	if got := mappingKeys(t, jobs[0]); !slices.Equal(got, []string{"approve-release"}) {
		t.Fatalf("first workflow job = %v, want approve-release", got)
	}
	hold := field(t, jobs[0], "approve-release")
	if got := mappingKeys(t, hold); !slices.Equal(got, []string{"filters", "type"}) {
		t.Fatalf("approve-release keys = %v, want exactly filters, type", got)
	}
	if got := text(t, hold, "type"); got != "approval" {
		t.Fatalf("approve-release type = %q, want approval", got)
	}
	assertTagOnlyFilters(t, hold)
	if got := mappingKeys(t, jobs[1]); !slices.Equal(got, []string{"release"}) {
		t.Fatalf("second workflow job = %v, want release", got)
	}
	job := field(t, jobs[1], "release")
	if got := mappingKeys(t, job); !slices.Equal(got, []string{"context", "filters", "requires"}) {
		t.Fatalf("workflow job keys = %v, want exactly context, filters, requires", got)
	}
	if got := strings_(t, job, "requires"); !slices.Equal(got, []string{"approve-release"}) {
		t.Fatalf("release requires = %v, want [approve-release]", got)
	}
	if got := strings_(t, job, "context"); !slices.Equal(got, []string{releaseContext}) {
		t.Fatalf("context = %v, want [%s] (holds the publishing tokens)", got, releaseContext)
	}
	assertTagOnlyFilters(t, job)
}

// assertTagOnlyFilters requires a workflow job to run for version tags and
// never for branches (CircleCI skips tag builds for jobs without tag filters).
func assertTagOnlyFilters(t *testing.T, job any) {
	t.Helper()
	if got := mappingKeys(t, job, "filters"); !slices.Equal(got, []string{"branches", "tags"}) {
		t.Fatalf("filters = %v, want exactly branches, tags", got)
	}
	if got := text(t, job, "filters", "branches", "ignore"); got != "/.*/" {
		t.Fatalf("filters.branches.ignore = %q, want /.*/ (never run for branches)", got)
	}
	if got := text(t, job, "filters", "tags", "only"); got != releaseTagFilter {
		t.Fatalf("filters.tags.only = %q, want %q", got, releaseTagFilter)
	}
}

func goModToolchain(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^toolchain go([0-9]+\.[0-9]+\.[0-9]+)$`).FindSubmatch(data)
	if match == nil {
		t.Fatal("go.mod must pin a toolchain goX.Y.Z")
	}
	return string(match[1])
}

// TestCircleCIReleaseJobUsesPinnedToolchainImage keeps the release build on
// the exact go.mod toolchain and on an immutable image digest.
func TestCircleCIReleaseJobUsesPinnedToolchainImage(t *testing.T) {
	job := field(t, loadReleaseConfig(t), "jobs", "release")
	if got := mappingKeys(t, job); !slices.Equal(got, []string{"docker", "environment", "resource_class", "steps"}) {
		t.Fatalf("release job keys = %v", got)
	}
	images := list(t, job, "docker")
	if len(images) != 1 {
		t.Fatalf("docker images = %d, want 1", len(images))
	}
	image := text(t, images[0], "image")
	want := regexp.MustCompile(`^cimg/go:` + regexp.QuoteMeta(goModToolchain(t)) + `@sha256:[0-9a-f]{64}$`)
	if !want.MatchString(image) {
		t.Fatalf("image = %q, want cimg/go:%s pinned by sha256 digest", image, goModToolchain(t))
	}
	if got := text(t, job, "environment", "GOTOOLCHAIN"); got != "local" {
		t.Fatalf("GOTOOLCHAIN = %q, want local", got)
	}
}

// TestCircleCIReleaseJobSteps fixes the job to checkout, verified tool
// installation, then the release script, so all logic lives in scripts that
// are linted and exercised below.
func TestCircleCIReleaseJobSteps(t *testing.T) {
	steps := list(t, loadReleaseConfig(t), "jobs", "release", "steps")
	if len(steps) != 3 || steps[0] != "checkout" {
		t.Fatalf("steps = %v, want checkout followed by two run steps", steps)
	}
	for index, command := range []string{".circleci/scripts/install-release-tools.sh", ".circleci/scripts/release.sh"} {
		step := steps[index+1]
		if got := mappingKeys(t, step); !slices.Equal(got, []string{"run"}) {
			t.Fatalf("step %d = %v, want a run step", index+1, got)
		}
		if got := text(t, step, "run", "command"); got != command {
			t.Fatalf("step %d command = %q, want %q", index+1, got, command)
		}
		if _, err := os.Stat(filepath.Join(repoRoot(t), command)); err != nil {
			t.Fatalf("step %d script: %v", index+1, err)
		}
	}
}

var pinnedTools = map[string]string{
	"GORELEASER_VERSION": "v2.18.3",
	"COSIGN_VERSION":     "v3.1.3",
	"SYFT_VERSION":       "v1.54.1",
}

// TestReleaseToolsArePinnedWithChecksums requires every release tool to be
// pinned to an exact version and verified against a recorded sha256.
func TestReleaseToolsArePinnedWithChecksums(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".circleci", "scripts", "install-release-tools.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for name, version := range pinnedTools {
		if !strings.Contains(script, "\n"+name+"="+version+"\n") {
			t.Errorf("install-release-tools.sh must pin %s=%s", name, version)
		}
		sha := strings.TrimSuffix(name, "_VERSION") + "_SHA256="
		if !regexp.MustCompile(`(?m)^` + sha + `[0-9a-f]{64}$`).MatchString(script) {
			t.Errorf("install-release-tools.sh must record %s<sha256>", sha)
		}
	}
}

// --- behavioral harness for release.sh ---------------------------------------

type releaseRun struct {
	output string
	calls  []string
	err    error
}

// runReleaseScript executes release.sh with stub git, make, goreleaser,
// circleci and cosign executables that log their arguments, so the script's
// gating and ordering are tested without publishing anything.
func runReleaseScript(t *testing.T, tag string, onMain bool) releaseRun {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("release.sh runs on the Linux release image")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls.log")
	stub := func(name, body string) {
		script := "#!/bin/sh\nprintf '%s' \"" + name + "\" >> \"$CALLS\"\nfor a in \"$@\"; do printf ' %s' \"$a\" >> \"$CALLS\"; done\nprintf '\\n' >> \"$CALLS\"\n" + body
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ancestor := "exit 1"
	if onMain {
		ancestor = "exit 0"
	}
	stub("git", `case "$1" in merge-base) `+ancestor+` ;; esac
exit 0
`)
	stub("make", "exit 0\n")
	stub("circleci", "printf 'stub-oidc-token'\n")
	stub("goreleaser", "exit 0\n")
	stub("cosign", "exit 0\n")
	cmd := exec.Command(bash, filepath.Join(repoRoot(t), ".circleci", "scripts", "release.sh"))
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"HOME=" + dir,
		"CALLS=" + log,
		"CIRCLE_TAG=" + tag,
		"CIRCLE_SHA1=0123456789abcdef0123456789abcdef01234567",
		"CIRCLE_PROJECT_ID=" + circleciProjectID,
		"GITHUB_TOKEN=stub-release-token",
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	runErr := cmd.Run()
	var calls []string
	if data, err := os.ReadFile(log); err == nil {
		calls = strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	return releaseRun{output: output.String(), calls: calls, err: runErr}
}

func callIndex(calls []string, prefix string) int {
	return slices.IndexFunc(calls, func(call string) bool { return strings.HasPrefix(call, prefix) })
}

func TestReleaseScriptRejectsInvalidTagsBeforeBuilding(t *testing.T) {
	for _, tag := range []string{"", "1.2.3", "v1.2", "v1.2.3;id", "v01.2.3", "release-v1.2.3", "v1.2.3-", "v1.2.3\nv1"} {
		run := runReleaseScript(t, tag, true)
		if run.err == nil {
			t.Errorf("tag %q: release.sh succeeded, want rejection", tag)
		}
		for _, tool := range []string{"make", "goreleaser", "cosign", "circleci"} {
			if callIndex(run.calls, tool) >= 0 {
				t.Errorf("tag %q: %s ran before the tag was validated: %v", tag, tool, run.calls)
			}
		}
	}
}

func TestReleaseScriptRejectsTagsNotOnMain(t *testing.T) {
	run := runReleaseScript(t, "v1.2.3", false)
	if run.err == nil || !strings.Contains(run.output, "not reachable from origin/main") {
		t.Fatalf("want rejection for a tag off main, got err=%v output=%s", run.err, run.output)
	}
	if callIndex(run.calls, "make") >= 0 || callIndex(run.calls, "goreleaser") >= 0 {
		t.Fatalf("build ran for a tag off main: %v", run.calls)
	}
}

func TestReleaseScriptGatesSignsPublishesAndVerifies(t *testing.T) {
	run := runReleaseScript(t, "v1.2.3-rc.1", true)
	if run.err != nil {
		t.Fatalf("release.sh failed: %v\n%s", run.err, run.output)
	}
	if strings.Contains(run.output, "stub-oidc-token") || strings.Contains(run.output, "stub-release-token") {
		t.Fatal("release.sh must not print credentials")
	}
	order := []string{
		"git fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main",
		"git merge-base --is-ancestor 0123456789abcdef0123456789abcdef01234567 refs/remotes/origin/main",
		"make ci",
		`circleci run oidc get --claims {"aud":"sigstore"} --root-issuer`,
		"goreleaser release --clean",
		"cosign verify-blob --bundle dist/checksums.txt.sigstore.json --certificate-identity " + releaseCertificateIdentity + " --certificate-oidc-issuer " + releaseOIDCIssuer + " dist/checksums.txt",
	}
	last := -1
	for _, want := range order {
		index := slices.Index(run.calls, want)
		if index < 0 {
			t.Fatalf("missing call %q in %v", want, run.calls)
		}
		if index <= last {
			t.Fatalf("call %q out of order in %v", want, run.calls)
		}
		last = index
	}
}

// TestInstallersVerifyTheReleasePipelineIdentity ties the signer (the
// release pipeline definition) to what both installers accept.
func TestInstallersVerifyTheReleasePipelineIdentity(t *testing.T) {
	for _, name := range []string{"install.sh", "install.ps1"} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		script := string(data)
		for _, want := range []string{releaseCertificateIdentity, releaseOIDCIssuer} {
			if !strings.Contains(script, want) {
				t.Errorf("%s must verify %s", name, want)
			}
		}
		for _, stale := range []string{"token.actions.githubusercontent.com", ".github/workflows/release.yml"} {
			if strings.Contains(script, stale) {
				t.Errorf("%s still references the GitHub Actions signer %s", name, stale)
			}
		}
	}
}
