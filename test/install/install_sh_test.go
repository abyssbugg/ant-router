// Package install exercises scripts/install.sh end to end against a local
// release fixture server. The tests never touch the network or the real
// install locations.
package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const testVersion = "v1.2.3"

// archiveEntry describes one tar member of a fake release archive.
type archiveEntry struct {
	name     string
	body     string
	linkname string // non-empty creates a symlink
}

func defaultEntries() []archiveEntry {
	return []archiveEntry{
		{name: "routerd", body: "#!/bin/sh\necho routerd\n"},
		{name: "routerctl", body: "#!/bin/sh\necho routerctl\n"},
		{name: "LICENSE", body: "license\n"},
	}
}

func buildArchive(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.linkname != "" {
			hdr = &tar.Header{Name: e.name, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: e.linkname}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.linkname == "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func archiveName(goos, arch string) string {
	return fmt.Sprintf("ant-router_%s_%s_%s.tar.gz", strings.TrimPrefix(testVersion, "v"), goos, arch)
}

// release is a fake GitHub release served from loopback.
type release struct {
	t        *testing.T
	server   *httptest.Server
	files    map[string][]byte // path -> body
	mu       sync.Mutex
	requests []string
}

// newRelease serves a well-formed release for every supported OS/arch.
// mutate may adjust files before serving.
func newRelease(t *testing.T, entries []archiveEntry, mutate func(files map[string][]byte)) *release {
	t.Helper()
	r := &release{t: t, files: map[string][]byte{}}
	var sums strings.Builder
	for _, goos := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name := archiveName(goos, arch)
			body := buildArchive(t, entries)
			r.files["/releases/download/"+testVersion+"/"+name] = body
			fmt.Fprintf(&sums, "%s  %s\n", sha256Hex(body), name)
		}
	}
	r.files["/releases/download/"+testVersion+"/checksums.txt"] = []byte(sums.String())
	r.files["/releases/download/"+testVersion+"/checksums.txt.sigstore.json"] = []byte(`{"bundle":"fake"}`)
	if mutate != nil {
		mutate(r.files)
	}
	r.server = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.server.Close)
	return r
}

func (r *release) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.requests = append(r.requests, req.URL.Path)
	r.mu.Unlock()
	if req.URL.Path == "/releases/latest" {
		http.Redirect(w, req, "/releases/tag/"+testVersion, http.StatusFound)
		return
	}
	if req.URL.Path == "/releases/tag/"+testVersion {
		_, _ = w.Write([]byte("<html>release v9.9.9 notes</html>"))
		return
	}
	body, ok := r.files[req.URL.Path]
	if !ok {
		http.NotFound(w, req)
		return
	}
	_, _ = w.Write(body)
}

func (r *release) requested(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.requests {
		if p == path {
			return true
		}
	}
	return false
}

func (r *release) checksumsPath() string {
	return "/releases/download/" + testVersion + "/checksums.txt"
}

// harness holds an isolated environment for one script run.
type harness struct {
	t          *testing.T
	script     string
	dir        string
	binDir     string
	installDir string
	cosignLog  string
}

func newHarness(t *testing.T, unameS, unameM string) *harness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install.sh targets macOS and Linux")
	}
	dir := t.TempDir()
	h := &harness{
		t:          t,
		script:     filepath.Join(repoRoot(t), "scripts", "install.sh"),
		dir:        dir,
		binDir:     filepath.Join(dir, "fakebin"),
		installDir: filepath.Join(dir, "install"),
		cosignLog:  filepath.Join(dir, "cosign.log"),
	}
	if err := os.Mkdir(h.binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.writeExe("uname", fmt.Sprintf(`#!/bin/sh
case "$1" in
  -s) echo %q ;;
  -m) echo %q ;;
  *) echo "fake uname: unsupported $1" >&2; exit 1 ;;
esac
`, unameS, unameM))
	return h
}

func (h *harness) writeExe(name, body string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.binDir, name), []byte(body), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

// installFakeCosign records its arguments and exits with the given status.
func (h *harness) installFakeCosign(exitCode int) {
	h.writeExe("cosign", fmt.Sprintf(`#!/bin/sh
for a in "$@"; do printf '%%s\n' "$a"; done > %q
exit %d
`, h.cosignLog, exitCode))
}

func (h *harness) run(args ...string) (string, error) {
	h.t.Helper()
	cmd := exec.Command("/bin/sh", append([]string{h.script}, args...)...)
	// Minimal PATH: fake shims first, then only system directories, so a
	// developer's real cosign is never picked up.
	cmd.Env = []string{
		"PATH=" + h.binDir + ":/usr/bin:/bin",
		"HOME=" + h.dir,
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (h *harness) assertInstalled(entries []archiveEntry) {
	h.t.Helper()
	for _, e := range entries {
		if e.name != "routerd" && e.name != "routerctl" {
			continue
		}
		p := filepath.Join(h.installDir, e.name)
		fi, err := os.Lstat(p)
		if err != nil {
			h.t.Fatalf("%s not installed: %v", e.name, err)
		}
		if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o755 {
			h.t.Fatalf("%s has mode %v, want regular 0755", e.name, fi.Mode())
		}
		got, err := os.ReadFile(p)
		if err != nil {
			h.t.Fatal(err)
		}
		if string(got) != e.body {
			h.t.Fatalf("%s content = %q, want %q", e.name, got, e.body)
		}
	}
	h.assertNoStagingFiles()
}

func (h *harness) assertNothingInstalled() {
	h.t.Helper()
	for _, bin := range []string{"routerd", "routerctl"} {
		if _, err := os.Lstat(filepath.Join(h.installDir, bin)); err == nil {
			h.t.Fatalf("%s must not be installed", bin)
		}
	}
	h.assertNoStagingFiles()
}

func (h *harness) assertNoStagingFiles() {
	h.t.Helper()
	matches, _ := filepath.Glob(filepath.Join(h.installDir, ".*.tmp.*"))
	if len(matches) != 0 {
		h.t.Fatalf("staging files left behind: %v", matches)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func expectFailure(t *testing.T, out string, err error, wantSubstring string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected failure, script succeeded:\n%s", out)
	}
	if !strings.Contains(out, wantSubstring) {
		t.Fatalf("output missing %q:\n%s", wantSubstring, out)
	}
}

func TestInstallScriptSyntax(t *testing.T) {
	h := newHarness(t, "Linux", "x86_64")
	if out, err := exec.Command("/bin/sh", "-n", h.script).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not installed")
	}
	if out, err := exec.Command("shellcheck", "-s", "sh", h.script).CombinedOutput(); err != nil {
		t.Fatalf("shellcheck: %v\n%s", err, out)
	}
}

func TestInstallScriptLatestViaRedirect(t *testing.T) {
	rel := newRelease(t, defaultEntries(), nil)
	h := newHarness(t, "Linux", "x86_64")
	out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	// The redirect target page mentions v9.9.9; the tag must come from the
	// redirect URL, not from scraping the body.
	if !strings.Contains(out, "ant-router v1.2.3 for linux/amd64") {
		t.Fatalf("expected v1.2.3 resolved from redirect:\n%s", out)
	}
	h.assertInstalled(defaultEntries())
}

func TestInstallScriptExplicitVersionPerPlatform(t *testing.T) {
	for _, tc := range []struct{ s, m, goos, arch string }{
		{"Linux", "x86_64", "linux", "amd64"},
		{"Linux", "aarch64", "linux", "arm64"},
		{"Darwin", "arm64", "darwin", "arm64"},
		{"Darwin", "x86_64", "darwin", "amd64"},
	} {
		t.Run(tc.goos+"_"+tc.arch, func(t *testing.T) {
			rel := newRelease(t, defaultEntries(), nil)
			h := newHarness(t, tc.s, tc.m)
			out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
			if err != nil {
				t.Fatalf("install failed: %v\n%s", err, out)
			}
			if !rel.requested("/releases/download/" + testVersion + "/" + archiveName(tc.goos, tc.arch)) {
				t.Fatalf("archive for %s/%s was not requested", tc.goos, tc.arch)
			}
			if rel.requested("/releases/latest") {
				t.Fatal("explicit --version must not resolve latest")
			}
			h.assertInstalled(defaultEntries())
		})
	}
}

func TestInstallScriptChecksumRejections(t *testing.T) {
	name := archiveName("linux", "amd64")
	for _, tc := range []struct {
		desc      string
		checksums func(good string) string
		want      string
	}{
		{"mismatch", func(string) string { return strings.Repeat("0", 64) + "  " + name + "\n" }, "checksum mismatch"},
		{"missing line", func(string) string { return "" }, "no checksum"},
		{"similar filenames only", func(good string) string {
			return good + "  " + name + ".bak\n" + good + "  x" + name + "\n" + good + "  " + strings.Replace(name, ".", "X", 1) + "\n"
		}, "no checksum"},
		{"duplicate entries", func(good string) string {
			return good + "  " + name + "\n" + strings.Repeat("a", 64) + "  " + name + "\n"
		}, "malformed or duplicate"},
		{"short hash", func(good string) string { return good[:63] + "  " + name + "\n" }, "malformed or duplicate"},
		{"uppercase hash", func(good string) string { return strings.ToUpper(good) + "  " + name + "\n" }, "malformed or duplicate"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var rel *release
			rel = newRelease(t, defaultEntries(), func(files map[string][]byte) {
				good := sha256Hex(files["/releases/download/"+testVersion+"/"+name])
				files["/releases/download/"+testVersion+"/checksums.txt"] = []byte(tc.checksums(good))
			})
			h := newHarness(t, "Linux", "x86_64")
			// A previously installed binary must survive a failed upgrade.
			if err := os.MkdirAll(h.installDir, 0o755); err != nil {
				t.Fatal(err)
			}
			old := filepath.Join(h.installDir, "routerd")
			if err := os.WriteFile(old, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
			expectFailure(t, out, err, tc.want)
			if got, _ := os.ReadFile(old); string(got) != "old" {
				t.Fatalf("existing routerd was modified: %q", got)
			}
			if _, err := os.Lstat(filepath.Join(h.installDir, "routerctl")); err == nil {
				t.Fatal("routerctl must not be installed")
			}
			h.assertNoStagingFiles()
		})
	}
}

func TestInstallScriptUnsupportedPlatform(t *testing.T) {
	for _, tc := range []struct{ s, m, want string }{
		{"FreeBSD", "x86_64", "unsupported operating system: FreeBSD"},
		{"Linux", "armv7l", "unsupported architecture: armv7l"},
		{"Darwin", "i386", "unsupported architecture: i386"},
	} {
		t.Run(tc.s+"_"+tc.m, func(t *testing.T) {
			rel := newRelease(t, defaultEntries(), nil)
			h := newHarness(t, tc.s, tc.m)
			out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
			expectFailure(t, out, err, tc.want)
			if len(rel.requests) != 0 {
				t.Fatalf("no requests expected, got %v", rel.requests)
			}
			h.assertNothingInstalled()
		})
	}
}

func TestInstallScriptArgumentValidation(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args []string
		want string
	}{
		{"version without v", []string{"--version", "1.2.3"}, "invalid version"},
		{"version path traversal", []string{"--version", "v1.2.3/../../x"}, "invalid version"},
		{"version with space", []string{"--version", "v1.2.3 x"}, "invalid version"},
		{"version missing value", []string{"--version"}, "--version requires a value"},
		{"install-dir missing value", []string{"--install-dir"}, "--install-dir requires a value"},
		{"unknown option", []string{"--bogus"}, "unknown option: --bogus"},
		{"plain http remote", []string{"--base-url", "http://example.com"}, "must use https"},
		{"file scheme", []string{"--base-url", "file:///etc"}, "must use https"},
		{"http loopback lookalike", []string{"--base-url", "http://127.0.0.1.evil.example"}, "must use https"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			h := newHarness(t, "Linux", "x86_64")
			args := append([]string{"--install-dir", h.installDir}, tc.args...)
			out, err := h.run(args...)
			expectFailure(t, out, err, tc.want)
			h.assertNothingInstalled()
		})
	}
}

func TestInstallScriptDryRun(t *testing.T) {
	rel := newRelease(t, defaultEntries(), nil)
	h := newHarness(t, "Linux", "x86_64")
	out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion, "--dry-run")
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, archiveName("linux", "amd64")) {
		t.Fatalf("dry run should name the archive:\n%s", out)
	}
	if len(rel.requests) != 0 {
		t.Fatalf("dry run must not download anything, got %v", rel.requests)
	}
	if _, err := os.Stat(h.installDir); err == nil {
		t.Fatal("dry run must not create the install directory")
	}
}

func TestInstallScriptCosignVerification(t *testing.T) {
	t.Run("invoked with exact arguments", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		h := newHarness(t, "Linux", "x86_64")
		h.installFakeCosign(0)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		logged, err := os.ReadFile(h.cosignLog)
		if err != nil {
			t.Fatalf("cosign was not invoked: %v", err)
		}
		args := strings.Split(strings.TrimSuffix(string(logged), "\n"), "\n")
		want := []string{
			"verify-blob",
			"--bundle", "<tmp>/checksums.txt.sigstore.json",
			"--certificate-identity", "https://github.com/abyssbugg/ant-router/.github/workflows/release.yml@refs/tags/v1.2.3",
			"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
			"<tmp>/checksums.txt",
		}
		if len(args) != len(want) {
			t.Fatalf("cosign args = %q, want %q", args, want)
		}
		for i := range want {
			got := args[i]
			if strings.HasPrefix(want[i], "<tmp>/") {
				if filepath.Base(got) != strings.TrimPrefix(want[i], "<tmp>/") || !filepath.IsAbs(got) {
					t.Fatalf("cosign arg %d = %q, want absolute path ending %q", i, got, want[i])
				}
				continue
			}
			if got != want[i] {
				t.Fatalf("cosign arg %d = %q, want %q", i, got, want[i])
			}
		}
		h.assertInstalled(defaultEntries())
	})

	t.Run("failure aborts install", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		h := newHarness(t, "Linux", "x86_64")
		h.installFakeCosign(1)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		expectFailure(t, out, err, "signature verification failed")
		h.assertNothingInstalled()
	})

	t.Run("missing bundle aborts when cosign present", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), func(files map[string][]byte) {
			delete(files, "/releases/download/"+testVersion+"/checksums.txt.sigstore.json")
		})
		h := newHarness(t, "Linux", "x86_64")
		h.installFakeCosign(0)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		expectFailure(t, out, err, "checksums.txt.sigstore.json")
		h.assertNothingInstalled()
	})

	t.Run("no-verify-signature skips cosign but still checks checksum", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		h := newHarness(t, "Linux", "x86_64")
		h.installFakeCosign(1)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion, "--no-verify-signature")
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		if _, err := os.Stat(h.cosignLog); err == nil {
			t.Fatal("cosign must not run with --no-verify-signature")
		}
		if !rel.requested(rel.checksumsPath()) {
			t.Fatal("checksums must still be downloaded")
		}
		h.assertInstalled(defaultEntries())
	})
}

func TestInstallScriptRejectsUnsafeArchives(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		entries []archiveEntry
		want    string
	}{
		{"symlinked binary", []archiveEntry{
			{name: "routerd", linkname: "/etc/passwd"},
			{name: "routerctl", body: "ctl"},
		}, "routerd in"},
		{"missing binary", []archiveEntry{
			{name: "routerd", body: "d"},
		}, "failed to extract"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			rel := newRelease(t, tc.entries, nil)
			h := newHarness(t, "Linux", "x86_64")
			out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
			expectFailure(t, out, err, tc.want)
			h.assertNothingInstalled()
		})
	}

	t.Run("extra members are not extracted outside the temp dir", func(t *testing.T) {
		entries := append(defaultEntries(), archiveEntry{name: "../escaped", body: "x"})
		rel := newRelease(t, entries, nil)
		h := newHarness(t, "Linux", "x86_64")
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		h.assertInstalled(defaultEntries())
		if _, err := os.Stat(filepath.Join(h.installDir, "LICENSE")); err == nil {
			t.Fatal("non-binary members must not be installed")
		}
	})
}
