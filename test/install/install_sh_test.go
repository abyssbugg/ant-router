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
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	// redirects maps a request path to a Location header served with 302.
	redirects map[string]string
	// truncated paths announce their full Content-Length but close the
	// connection after half of the body.
	truncated map[string]bool
}

// newRelease serves a well-formed release for every supported OS/arch.
// mutate may adjust files before serving.
func newRelease(t *testing.T, entries []archiveEntry, mutate func(files map[string][]byte)) *release {
	t.Helper()
	r := &release{t: t, files: map[string][]byte{}, redirects: map[string]string{}, truncated: map[string]bool{}}
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
	location, redirect := r.redirects[req.URL.Path]
	truncate := r.truncated[req.URL.Path]
	r.mu.Unlock()
	if redirect {
		http.Redirect(w, req, location, http.StatusFound)
		return
	}
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
	if truncate {
		// A short body under a full Content-Length makes the server close
		// the connection mid-transfer after the 200 headers were sent.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body[:len(body)/2])
		return
	}
	_, _ = w.Write(body)
}

// truncate makes path send its 200 headers and then drop the connection
// halfway through the body.
func (r *release) truncate(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.truncated[path] = true
}

// requestsTo counts the requests made for path.
func (r *release) requestsTo(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, p := range r.requests {
		if p == path {
			n++
		}
	}
	return n
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

// redirect makes path answer with a 302 to location.
func (r *release) redirect(path, location string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.redirects[path] = location
}

// requestCount reads the request log under the mutex; the handler goroutines
// append to it concurrently with the test goroutine.
func (r *release) requestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *release) requestLog() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.requests...)
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
	tmpDir     string
	cosignLog  string
	// sysPath holds the system directories placed after the shims.
	sysPath string
	// env overrides or removes (empty value) default environment entries.
	env map[string]string
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
		tmpDir:     filepath.Join(dir, "tmp"),
		cosignLog:  filepath.Join(dir, "cosign.log"),
		sysPath:    "/usr/bin:/bin",
		env:        map[string]string{},
	}
	for _, d := range []string{h.binDir, h.tmpDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Signature verification is mandatory, so every run gets a passing fake
	// cosign by default. It also shadows any real cosign on the system PATH.
	h.installFakeCosign(0)
	h.writeExe("uname", fmt.Sprintf(`#!/bin/sh
case "$1" in
  -s) echo %q ;;
  -m) echo %q ;;
  *) echo "fake uname: unsupported $1" >&2; exit 1 ;;
esac
`, unameS, unameM))
	return h
}

// useWget hides curl so install.sh must download with wget: the system
// directories are replaced by one directory of symlinks that omits curl.
func (h *harness) useWget() {
	h.t.Helper()
	wget := lookTool(h.t, "wget")
	h.hideDownloaders()
	// The shim records each call so tests can prove wget did the download.
	h.writeExe("wget", fmt.Sprintf("#!/bin/sh\necho call >> %q\nexec %q \"$@\"\n", h.wgetLog(), wget))
}

// useLimitedWget hides curl and installs a BusyBox-style wget whose help
// lacks the long options fetch_once needs and which rejects them.
func (h *harness) useLimitedWget() {
	h.t.Helper()
	h.hideDownloaders()
	h.writeExe("wget", fmt.Sprintf(`#!/bin/sh
echo call >> %q
case "$1" in
  --help) echo "BusyBox v1.36.1 multi-call binary." >&2; echo "Usage: wget [-cqS] [-O FILE] [-T SEC] URL..." >&2; exit 1 ;;
  *) echo "wget: unrecognized option '$1'" >&2; exit 1 ;;
esac
`, h.wgetLog()))
}

// hideDownloaders replaces the system directories with one directory of
// symlinks that omits curl and wget.
func (h *harness) hideDownloaders() {
	h.t.Helper()
	sys := filepath.Join(h.dir, "sysbin")
	if err := os.Mkdir(sys, 0o755); err != nil {
		h.t.Fatal(err)
	}
	for _, d := range strings.Split(h.sysPath, ":") {
		entries, err := os.ReadDir(d)
		if err != nil {
			h.t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if name == "curl" || name == "wget" {
				continue
			}
			dst := filepath.Join(sys, name)
			if _, err := os.Lstat(dst); err == nil {
				continue // earlier PATH entries win, as with the real PATH
			}
			if err := os.Symlink(filepath.Join(d, name), dst); err != nil {
				h.t.Fatal(err)
			}
		}
	}
	h.sysPath = sys
}

func (h *harness) wgetLog() string { return filepath.Join(h.dir, "wget.log") }

func (h *harness) wgetCalls() int {
	b, err := os.ReadFile(h.wgetLog())
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "call\n")
}

func (h *harness) writeExe(name, body string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.binDir, name), []byte(body), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

// removeFakeCosign exercises the missing-cosign path. It skips when a real
// cosign in the system directories on the harness PATH would be found instead.
func (h *harness) removeFakeCosign() {
	h.t.Helper()
	for _, p := range []string{"/usr/bin/cosign", "/bin/cosign"} {
		if _, err := os.Stat(p); err == nil {
			h.t.Skipf("system cosign at %s shadows the missing-cosign path", p)
		}
	}
	if err := os.Remove(filepath.Join(h.binDir, "cosign")); err != nil {
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
	env := map[string]string{
		"PATH":   h.binDir + ":" + h.sysPath,
		"HOME":   h.dir,
		"TMPDIR": h.tmpDir,
	}
	for k, v := range h.env {
		if v == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}
	cmd.Env = nil
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
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
	for _, pattern := range []string{".*.tmp.*", ".*.bak.*"} {
		matches, _ := filepath.Glob(filepath.Join(h.installDir, pattern))
		if len(matches) != 0 {
			h.t.Fatalf("staging or backup files left behind: %v", matches)
		}
	}
	if entries, _ := os.ReadDir(h.tmpDir); len(entries) != 0 {
		h.t.Fatalf("temporary files left behind in TMPDIR: %v", entries)
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
	shellcheck := lookTool(t, "shellcheck")
	if out, err := exec.Command(shellcheck, "-s", "sh", h.script).CombinedOutput(); err != nil {
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
			if n := rel.requestCount(); n != 0 {
				t.Fatalf("no requests expected, got %v", rel.requestLog())
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
		{"http loopback userinfo", []string{"--base-url", "http://localhost:80@evil.example"}, "must use https"},
		{"https userinfo", []string{"--base-url", "https://user:pw@github.com/abyssbugg/ant-router"}, "must not contain credentials"},
		{"https token userinfo", []string{"--base-url", "https://token@github.com/abyssbugg/ant-router"}, "must not contain credentials"},
		{"http loopback bad port", []string{"--base-url", "http://127.0.0.1:8x"}, "must use https"},
		{"http loopback empty port", []string{"--base-url", "http://127.0.0.1:"}, "must use https"},
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
	if n := rel.requestCount(); n != 0 {
		t.Fatalf("dry run must not download anything, got %v", rel.requestLog())
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
			"--certificate-identity", "https://circleci.com/api/v2/projects/cc5f04e8-66e9-4f18-91b0-71c671674d14/pipeline-definitions/b37a0ec3-4a25-4686-b480-6b4eda16099f",
			"--certificate-oidc-issuer", "https://oidc.circleci.com",
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

	for _, optOut := range []struct {
		desc string
		args []string
		env  map[string]string
	}{
		{"no-verify-signature flag", []string{"--no-verify-signature"}, nil},
		{"insecure env opt-out", nil, map[string]string{"ANT_ROUTER_INSECURE_SKIP_SIGNATURE": "1"}},
	} {
		t.Run(optOut.desc+" skips cosign with a warning but still checks checksum", func(t *testing.T) {
			rel := newRelease(t, defaultEntries(), nil)
			h := newHarness(t, "Linux", "x86_64")
			h.installFakeCosign(1)
			for k, v := range optOut.env {
				h.env[k] = v
			}
			args := append([]string{"--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion}, optOut.args...)
			out, err := h.run(args...)
			if err != nil {
				t.Fatalf("install failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "WARNING: signature verification is disabled") {
				t.Fatalf("skipping verification must warn:\n%s", out)
			}
			if _, err := os.Stat(h.cosignLog); err == nil {
				t.Fatal("cosign must not run when verification is disabled")
			}
			if !rel.requested(rel.checksumsPath()) {
				t.Fatal("checksums must still be downloaded")
			}
			h.assertInstalled(defaultEntries())
		})
	}

	t.Run("missing cosign fails before any download", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		h := newHarness(t, "Linux", "x86_64")
		h.removeFakeCosign()
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		expectFailure(t, out, err, "cosign is required")
		if n := rel.requestCount(); n != 0 {
			t.Fatalf("no requests expected, got %v", rel.requestLog())
		}
		h.assertNothingInstalled()
	})

	t.Run("env opt-out requires exactly 1", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		h := newHarness(t, "Linux", "x86_64")
		h.removeFakeCosign()
		h.env["ANT_ROUTER_INSECURE_SKIP_SIGNATURE"] = "yes"
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		expectFailure(t, out, err, "cosign is required")
		h.assertNothingInstalled()
	})
}

func TestInstallScriptRedirects(t *testing.T) {
	asset := "/releases/download/" + testVersion + "/" + archiveName("linux", "amd64")

	t.Run("loopback asset redirect is followed", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), func(files map[string][]byte) {
			files["/objects/archive"] = files[asset]
		})
		rel.redirect(asset, "/objects/archive")
		h := newHarness(t, "Linux", "x86_64")
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		if !rel.requested("/objects/archive") {
			t.Fatal("redirect target was not requested")
		}
		h.assertInstalled(defaultEntries())
	})

	for _, tc := range []struct {
		desc, path, location string
	}{
		{"asset to remote http", asset, "http://example.invalid/archive"},
		{"asset to userinfo lookalike", asset, "http://localhost:80@example.invalid/archive"},
		{"checksums to remote http", "/releases/download/" + testVersion + "/checksums.txt", "http://example.invalid/checksums.txt"},
		{"latest to remote http", "/releases/latest", "http://example.invalid/releases/tag/" + testVersion},
		{"asset to file scheme", asset, "file:///etc/passwd"},
	} {
		t.Run(tc.desc+" is rejected", func(t *testing.T) {
			rel := newRelease(t, defaultEntries(), nil)
			rel.redirect(tc.path, tc.location)
			h := newHarness(t, "Linux", "x86_64")
			args := []string{"--base-url", rel.server.URL, "--install-dir", h.installDir}
			if tc.path != "/releases/latest" {
				args = append(args, "--version", testVersion)
			}
			out, err := h.run(args...)
			if err == nil {
				t.Fatalf("expected failure, script succeeded:\n%s", out)
			}
			if !strings.Contains(out, "refusing to download") && !strings.Contains(out, "unsupported redirect target") {
				t.Fatalf("insecure redirect not rejected explicitly:\n%s", out)
			}
			h.assertNothingInstalled()
		})
	}

	t.Run("redirect loop is bounded", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		rel.redirect(asset, asset)
		h := newHarness(t, "Linux", "x86_64")
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		expectFailure(t, out, err, "too many redirects")
		// The first request plus at most 10 followed redirects.
		if n := rel.requestsTo(asset); n != 11 {
			t.Fatalf("archive requested %d times, want exactly 11", n)
		}
		h.assertNothingInstalled()
	})
}

// TestInstallScriptWithWget runs the download paths with curl hidden, so the
// wget implementation of fetch_once and its redirect parsing are exercised.
func TestInstallScriptWithWget(t *testing.T) {
	asset := "/releases/download/" + testVersion + "/" + archiveName("linux", "amd64")
	newWgetHarness := func(t *testing.T) *harness {
		h := newHarness(t, "Linux", "x86_64")
		h.useWget()
		return h
	}
	assertUsedWget := func(t *testing.T, h *harness) {
		t.Helper()
		if h.wgetCalls() == 0 {
			t.Fatal("wget was never invoked; the curl path ran instead")
		}
	}

	t.Run("latest resolved from redirect", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		h := newWgetHarness(t)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, "ant-router v1.2.3 for linux/amd64") {
			t.Fatalf("expected v1.2.3 resolved from redirect:\n%s", out)
		}
		assertUsedWget(t, h)
		h.assertInstalled(defaultEntries())
	})

	t.Run("loopback asset redirect is followed", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), func(files map[string][]byte) {
			files["/objects/archive"] = files[asset]
		})
		rel.redirect(asset, "/objects/archive")
		h := newWgetHarness(t)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		if !rel.requested("/objects/archive") {
			t.Fatal("redirect target was not requested")
		}
		assertUsedWget(t, h)
		h.assertInstalled(defaultEntries())
	})

	for _, tc := range []struct{ desc, location string }{
		{"remote http", "http://example.invalid/archive"},
		{"userinfo lookalike", "http://localhost:80@example.invalid/archive"},
		{"file scheme", "file:///etc/passwd"},
	} {
		t.Run("redirect to "+tc.desc+" is rejected", func(t *testing.T) {
			rel := newRelease(t, defaultEntries(), nil)
			rel.redirect(asset, tc.location)
			h := newWgetHarness(t)
			out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
			if err == nil {
				t.Fatalf("expected failure, script succeeded:\n%s", out)
			}
			if !strings.Contains(out, "refusing to download") && !strings.Contains(out, "unsupported redirect target") {
				t.Fatalf("insecure redirect not rejected explicitly:\n%s", out)
			}
			assertUsedWget(t, h)
			h.assertNothingInstalled()
		})
	}

	t.Run("redirect loop is bounded", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		rel.redirect(asset, asset)
		h := newWgetHarness(t)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		expectFailure(t, out, err, "too many redirects")
		// The first request plus at most 10 followed redirects.
		if n := rel.requestsTo(asset); n != 11 {
			t.Fatalf("archive requested %d times, want exactly 11", n)
		}
		assertUsedWget(t, h)
		h.assertNothingInstalled()
	})

	t.Run("truncated 200 body is a download failure", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), nil)
		rel.truncate(asset)
		h := newWgetHarness(t)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		expectFailure(t, out, err, "failed to download")
		if strings.Contains(out, "checksum mismatch") {
			t.Fatalf("network truncation reported as a checksum mismatch:\n%s", out)
		}
		assertUsedWget(t, h)
		h.assertNothingInstalled()
	})

	t.Run("missing asset fails", func(t *testing.T) {
		rel := newRelease(t, defaultEntries(), func(files map[string][]byte) {
			delete(files, asset)
		})
		h := newWgetHarness(t)
		out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
		if err == nil {
			t.Fatalf("expected failure for a 404 asset:\n%s", out)
		}
		assertUsedWget(t, h)
		h.assertNothingInstalled()
	})
}

// TestInstallScriptUnreachableHost requires a clear download error, not an
// empty HTTP status, when no server answers (curl and wget).
// A wget without --max-redirect/--timeout/--tries (e.g. BusyBox) cannot
// perform fetch_once; the installer must say so instead of reporting a
// generic download failure.
func TestInstallScriptRejectsWgetWithoutRequiredOptions(t *testing.T) {
	h := newHarness(t, "Linux", "x86_64")
	h.useLimitedWget()
	out, err := h.run("--install-dir", h.installDir, "--version", testVersion)
	expectFailure(t, out, err, "curl or a wget supporting --max-redirect, --timeout and --tries is required")
	if strings.Contains(out, "failed to download") {
		t.Fatalf("installer attempted a download with an incompatible wget:\n%s", out)
	}
	h.assertNothingInstalled()
}

func TestInstallScriptUnreachableHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	for _, downloader := range []string{"curl", "wget"} {
		t.Run(downloader, func(t *testing.T) {
			h := newHarness(t, "Linux", "x86_64")
			if downloader == "wget" {
				h.useWget()
			}
			out, err := h.run("--base-url", base, "--install-dir", h.installDir, "--version", testVersion)
			expectFailure(t, out, err, "failed to download "+base+"/releases/download/")
			h.assertNothingInstalled()
		})
	}
}

func TestInstallScriptInstallDirDefaults(t *testing.T) {
	t.Run("HOME unset and no install dir fails clearly", func(t *testing.T) {
		h := newHarness(t, "Linux", "x86_64")
		h.env["HOME"] = ""
		out, err := h.run("--version", testVersion, "--dry-run")
		expectFailure(t, out, err, "install directory is empty")
	})
	t.Run("ANT_ROUTER_INSTALL_DIR wins over HOME", func(t *testing.T) {
		h := newHarness(t, "Linux", "x86_64")
		h.env["ANT_ROUTER_INSTALL_DIR"] = h.installDir
		out, err := h.run("--version", testVersion, "--dry-run")
		if err != nil || !strings.Contains(out, "into "+h.installDir) {
			t.Fatalf("expected ANT_ROUTER_INSTALL_DIR to be used: %v\n%s", err, out)
		}
	})
	t.Run("HOME default", func(t *testing.T) {
		h := newHarness(t, "Linux", "x86_64")
		out, err := h.run("--version", testVersion, "--dry-run")
		if err != nil || !strings.Contains(out, "into "+filepath.Join(h.dir, ".local", "bin")) {
			t.Fatalf("expected $HOME/.local/bin: %v\n%s", err, out)
		}
	})
}

func TestInstallScriptRejectsDirectoryDestination(t *testing.T) {
	rel := newRelease(t, defaultEntries(), nil)
	h := newHarness(t, "Linux", "x86_64")
	if err := os.MkdirAll(filepath.Join(h.installDir, "routerctl"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
	expectFailure(t, out, err, "Error: "+filepath.Join(h.installDir, "routerctl")+" is a directory")
	if _, err := os.Lstat(filepath.Join(h.installDir, "routerd")); err == nil {
		t.Fatal("routerd must not be installed when routerctl cannot be")
	}
	h.assertNoStagingFiles()
}

func TestInstallScriptRollsBackOnPartialFailure(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		name := "upgrade restores previous binaries"
		if fresh {
			name = "fresh install removes the first binary"
		}
		t.Run(name, func(t *testing.T) {
			rel := newRelease(t, defaultEntries(), nil)
			h := newHarness(t, "Linux", "x86_64")
			if !fresh {
				if err := os.MkdirAll(h.installDir, 0o755); err != nil {
					t.Fatal(err)
				}
				for _, bin := range []string{"routerd", "routerctl"} {
					if err := os.WriteFile(filepath.Join(h.installDir, bin), []byte("old "+bin), 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}
			// mv fails only when committing the staged routerctl, i.e. after
			// routerd has already been replaced.
			h.writeExe("mv", `#!/bin/sh
for a in "$@"; do last="$a"; done
case "$*" in
  *.routerctl.tmp.*) case "$last" in */routerctl) echo "fake mv: refused" >&2; exit 1 ;; esac ;;
esac
exec /bin/mv "$@"
`)
			out, err := h.run("--base-url", rel.server.URL, "--install-dir", h.installDir, "--version", testVersion)
			expectFailure(t, out, err, "failed to install routerctl")
			for _, bin := range []string{"routerd", "routerctl"} {
				got, err := os.ReadFile(filepath.Join(h.installDir, bin))
				if fresh {
					if err == nil {
						t.Fatalf("%s must be rolled back on a fresh install, found %q", bin, got)
					}
					continue
				}
				if string(got) != "old "+bin {
					t.Fatalf("%s = %q, want restored %q", bin, got, "old "+bin)
				}
			}
			h.assertNoStagingFiles()
		})
	}
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
		// The installer extracts into $TMPDIR/<random>/extract, so
		// ../../escaped would land directly in the harness TMPDIR.
		entries := append(defaultEntries(), archiveEntry{name: "../../escaped", body: "x"})
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
		for _, p := range []string{filepath.Join(h.tmpDir, "escaped"), filepath.Join(h.dir, "escaped")} {
			if _, err := os.Lstat(p); err == nil {
				t.Fatalf("archive member escaped to %s", p)
			}
		}
	})
}
