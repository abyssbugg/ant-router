package install

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// --- static analysis (always runs) ---------------------------------------

// ps1ForbiddenConstructs are constructs the Windows installer must never
// contain: dynamic code execution, policy or Defender tampering, hidden
// processes, persistent environment/registry edits, and TLS bypasses.
var ps1ForbiddenConstructs = []struct {
	name    string
	pattern *regexp.Regexp
	sample  string
}{
	{"Invoke-Expression", regexp.MustCompile(`(?i)\binvoke-expression\b`), "Invoke-Expression $x"},
	{"iex alias", regexp.MustCompile(`(?i)(^|[^a-z0-9_-])iex([^a-z0-9_-]|$)`), "irm $u | iex"},
	{"EncodedCommand", regexp.MustCompile(`(?i)-e(nc(odedcommand)?|c)\b`), "pwsh -enc AAAA"},
	{"ExecutionPolicy", regexp.MustCompile(`(?i)executionpolicy`), "Set-ExecutionPolicy Bypass"},
	{"hidden window", regexp.MustCompile(`(?i)-windowstyle\s+hidden`), "-WindowStyle Hidden"},
	{"Start-Process", regexp.MustCompile(`(?i)\bstart-process\b`), "Start-Process x"},
	{"DownloadString", regexp.MustCompile(`(?i)downloadstring`), "(New-Object Net.WebClient).DownloadString($u)"},
	{"Defender exclusion", regexp.MustCompile(`(?i)\b(add|set)-mppreference\b`), "Add-MpPreference -ExclusionPath C:\\"},
	{"Unblock-File", regexp.MustCompile(`(?i)\bunblock-file\b`), "Unblock-File x"},
	{"persistent env var", regexp.MustCompile(`(?i)setenvironmentvariable`), "[Environment]::SetEnvironmentVariable('PATH', $p, 'User')"},
	{"registry provider", regexp.MustCompile(`(?i)\b(hkcu|hklm|registry)::?`), "Set-ItemProperty HKCU:\\Environment"},
	{"certificate bypass", regexp.MustCompile(`(?i)skipcertificatecheck|servercertificatevalidationcallback`), "-SkipCertificateCheck"},
	{"script block from string", regexp.MustCompile(`(?i)\[scriptblock\]::create`), "[ScriptBlock]::Create($s)"},
	{"Add-Type source", regexp.MustCompile(`(?i)add-type\s+(-typedefinition|-memberdefinition)`), "Add-Type -TypeDefinition $cs"},
}

// ps1RequiredConstructs pin the security-relevant behavior of the installer.
var ps1RequiredConstructs = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"strict mode", regexp.MustCompile(`(?m)^Set-StrictMode -Version Latest$`)},
	{"stop on error", regexp.MustCompile(`(?m)^\$ErrorActionPreference = 'Stop'$`)},
	{"TLS 1.2 on Windows PowerShell", regexp.MustCompile(`\[Net\.SecurityProtocolType\]::Tls12`)},
	{"basic parsing", regexp.MustCompile(`-UseBasicParsing`)},
	{"SHA-256 file hash", regexp.MustCompile(`Get-FileHash -LiteralPath \$archivePath -Algorithm SHA256`)},
	{"case-sensitive hash format", regexp.MustCompile(`-cnotmatch '\^\[0-9a-f\]\{64\}\$'`)},
	{"case-sensitive hash compare", regexp.MustCompile(`\$actual -cne \$expected`)},
	{"exact checksum filename", regexp.MustCompile(`\$name -ceq \$archive`)},
	{"cosign identity", regexp.MustCompile(regexp.QuoteMeta(`"$RepoUrl/.github/workflows/release.yml@refs/tags/"`))},
	{"cosign issuer", regexp.MustCompile(regexp.QuoteMeta(`'https://token.actions.githubusercontent.com'`))},
	{"cosign verify-blob", regexp.MustCompile(`verify-blob`)},
	{"zip assembly loaded", regexp.MustCompile(`Add-Type -AssemblyName System\.IO\.Compression\.FileSystem`)},
	{"per-entry extraction", regexp.MustCompile(`ZipFileExtensions\]::ExtractToFile`)},
	{"https-only base URL", regexp.MustCompile(`\$parsedBase\.Scheme -eq 'https'`)},
}

func readPS1(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "install.ps1"))
	if err != nil {
		t.Fatalf("read scripts/install.ps1: %v", err)
	}
	return string(data)
}

func TestInstallPS1ForbiddenConstructs(t *testing.T) {
	script := readPS1(t)
	for _, f := range ps1ForbiddenConstructs {
		// Every pattern must detect its own sample, so a broken regex
		// cannot silently pass the scan below.
		if !f.pattern.MatchString(f.sample) {
			t.Errorf("pattern %q does not detect its sample %q", f.name, f.sample)
		}
		if loc := f.pattern.FindStringIndex(script); loc != nil {
			line := strings.Count(script[:loc[0]], "\n") + 1
			t.Errorf("forbidden construct %q at scripts/install.ps1:%d", f.name, line)
		}
	}
	// Extracting the whole archive would write members other than the two
	// binaries.
	if strings.Contains(script, "ExtractToDirectory") || regexp.MustCompile(`(?i)\bexpand-archive\b`).MatchString(script) {
		t.Error("installer must not extract the whole archive")
	}
	// $Host is a read-only automatic variable; assigning it throws.
	if regexp.MustCompile(`(?i)\$host\s*=`).MatchString(script) {
		t.Error("installer assigns the read-only automatic variable $Host")
	}
}

func TestInstallPS1RequiredConstructs(t *testing.T) {
	script := readPS1(t)
	for _, r := range ps1RequiredConstructs {
		if !r.pattern.MatchString(script) {
			t.Errorf("required construct %q not found", r.name)
		}
	}
}

func TestInstallPS1IdentityMatchesShellInstaller(t *testing.T) {
	sh, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"/.github/workflows/release.yml@refs/tags/",
		"https://token.actions.githubusercontent.com",
		"https://github.com/abyssbugg/ant-router",
	} {
		if !strings.Contains(string(sh), s) || !strings.Contains(readPS1(t), s) {
			t.Errorf("installers disagree on %q", s)
		}
	}
}

// --- dynamic tests (require pwsh) ----------------------------------------

type zipEntry struct{ name, body string }

func defaultZipEntries() []zipEntry {
	return []zipEntry{
		{"routerd.exe", "routerd-binary"},
		{"routerctl.exe", "routerctl-binary"},
		{"LICENSE", "license"},
	}
}

func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func windowsArchiveName(arch string) string {
	return fmt.Sprintf("ant-router_%s_windows_%s.zip", strings.TrimPrefix(testVersion, "v"), arch)
}

type zipRelease struct {
	server   *httptest.Server
	files    map[string][]byte
	mu       sync.Mutex
	requests []string
}

func newZipRelease(t *testing.T, entries []zipEntry, mutate func(map[string][]byte)) *zipRelease {
	t.Helper()
	r := &zipRelease{files: map[string][]byte{}}
	var sums strings.Builder
	for _, arch := range []string{"amd64", "arm64"} {
		body := buildZip(t, entries)
		r.files["/releases/download/"+testVersion+"/"+windowsArchiveName(arch)] = body
		fmt.Fprintf(&sums, "%s  %s\n", sha256Hex(body), windowsArchiveName(arch))
	}
	r.files["/releases/download/"+testVersion+"/checksums.txt"] = []byte(sums.String())
	r.files["/releases/download/"+testVersion+"/checksums.txt.sigstore.json"] = []byte(`{"bundle":"fake"}`)
	if mutate != nil {
		mutate(r.files)
	}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.requests = append(r.requests, req.URL.Path)
		r.mu.Unlock()
		switch req.URL.Path {
		case "/releases/latest":
			http.Redirect(w, req, "/releases/tag/"+testVersion, http.StatusFound)
			return
		case "/releases/tag/" + testVersion:
			_, _ = w.Write([]byte("<html>release v9.9.9 notes</html>"))
			return
		}
		body, ok := r.files[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *zipRelease) requestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *zipRelease) requested(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.requests {
		if p == path {
			return true
		}
	}
	return false
}

type ps1Harness struct {
	t          *testing.T
	pwsh       string
	dir        string
	binDir     string
	installDir string
	cosignLog  string
	arch       string
}

func newPS1Harness(t *testing.T, processorArch string) *ps1Harness {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not installed")
	}
	dir := t.TempDir()
	h := &ps1Harness{
		t:          t,
		pwsh:       pwsh,
		dir:        dir,
		binDir:     filepath.Join(dir, "fakebin"),
		installDir: filepath.Join(dir, "install"),
		cosignLog:  filepath.Join(dir, "cosign.log"),
		arch:       processorArch,
	}
	if err := os.Mkdir(h.binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

// installFakeCosign places a cosign shim first on PATH that records one
// argument per line and exits with exitCode.
func (h *ps1Harness) installFakeCosign(exitCode int) {
	h.t.Helper()
	if runtime.GOOS == "windows" {
		body := fmt.Sprintf("@echo off\r\n(for %%%%a in (%%*) do @echo %%%%~a) > %q\r\nexit /b %d\r\n", h.cosignLog, exitCode)
		if err := os.WriteFile(filepath.Join(h.binDir, "cosign.cmd"), []byte(body), 0o755); err != nil {
			h.t.Fatal(err)
		}
		return
	}
	body := fmt.Sprintf("#!/bin/sh\nfor a in \"$@\"; do printf '%%s\\n' \"$a\"; done > %q\nexit %d\n", h.cosignLog, exitCode)
	if err := os.WriteFile(filepath.Join(h.binDir, "cosign"), []byte(body), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h *ps1Harness) run(args ...string) (string, error) {
	h.t.Helper()
	cmd := exec.Command(h.pwsh, append([]string{"-NoProfile", "-NonInteractive", "-File",
		filepath.Join(repoRoot(h.t), "scripts", "install.ps1")}, args...)...)
	var env []string
	for _, kv := range os.Environ() {
		k := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		if k == "PATH" || k == "PROCESSOR_ARCHITECTURE" || k == "PROCESSOR_ARCHITEW6432" || k == "ANT_ROUTER_INSTALL_DIR" {
			continue
		}
		env = append(env, kv)
	}
	// Keep only system directories after the shims so a real cosign is
	// never picked up.
	system := "/usr/bin:/bin"
	if runtime.GOOS == "windows" {
		system = os.Getenv("SystemRoot") + `\System32`
	}
	env = append(env,
		"PATH="+h.binDir+string(os.PathListSeparator)+filepath.Dir(h.pwsh)+string(os.PathListSeparator)+system,
		"PROCESSOR_ARCHITECTURE="+h.arch,
	)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (h *ps1Harness) assertInstalled() {
	h.t.Helper()
	for _, e := range defaultZipEntries()[:2] {
		got, err := os.ReadFile(filepath.Join(h.installDir, e.name))
		if err != nil {
			h.t.Fatalf("%s not installed: %v", e.name, err)
		}
		if string(got) != e.body {
			h.t.Fatalf("%s content = %q, want %q", e.name, got, e.body)
		}
	}
	if _, err := os.Stat(filepath.Join(h.installDir, "LICENSE")); err == nil {
		h.t.Fatal("LICENSE must not be installed")
	}
	h.assertNoStaging()
}

func (h *ps1Harness) assertNoStaging() {
	h.t.Helper()
	if m, _ := filepath.Glob(filepath.Join(h.installDir, ".*.tmp.*")); len(m) != 0 {
		h.t.Fatalf("staging files left behind: %v", m)
	}
}

func (h *ps1Harness) commonArgs(rel *zipRelease) []string {
	return []string{"-BaseUrl", rel.server.URL, "-InstallDir", h.installDir}
}

func TestInstallPS1LatestViaRedirect(t *testing.T) {
	h := newPS1Harness(t, "AMD64")
	rel := newZipRelease(t, defaultZipEntries(), nil)
	out, err := h.run(h.commonArgs(rel)...)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ant-router v1.2.3 for windows/amd64") {
		t.Fatalf("version must come from the redirect target:\n%s", out)
	}
	h.assertInstalled()
}

func TestInstallPS1ExplicitVersionPerArch(t *testing.T) {
	for _, tc := range []struct{ env, arch string }{{"AMD64", "amd64"}, {"ARM64", "arm64"}} {
		t.Run(tc.arch, func(t *testing.T) {
			h := newPS1Harness(t, tc.env)
			rel := newZipRelease(t, defaultZipEntries(), nil)
			out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
			if err != nil {
				t.Fatalf("install failed: %v\n%s", err, out)
			}
			if !rel.requested("/releases/download/" + testVersion + "/" + windowsArchiveName(tc.arch)) {
				t.Fatalf("archive for %s not requested", tc.arch)
			}
			if rel.requested("/releases/latest") {
				t.Fatal("explicit -Version must not resolve latest")
			}
			h.assertInstalled()
		})
	}
}

func TestInstallPS1ChecksumRejections(t *testing.T) {
	name := windowsArchiveName("amd64")
	for _, tc := range []struct {
		desc string
		sums func(good string) string
		want string
	}{
		{"mismatch", func(string) string { return strings.Repeat("0", 64) + "  " + name + "\n" }, "checksum mismatch"},
		{"missing", func(string) string { return "" }, "no checksum"},
		{"similar names only", func(g string) string { return g + "  " + name + ".bak\n" + g + "  x" + name + "\n" }, "no checksum"},
		{"duplicate", func(g string) string { return g + "  " + name + "\n" + strings.Repeat("a", 64) + "  " + name + "\n" }, "malformed or duplicate"},
		{"uppercase", func(g string) string { return strings.ToUpper(g) + "  " + name + "\n" }, "malformed or duplicate"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			h := newPS1Harness(t, "AMD64")
			rel := newZipRelease(t, defaultZipEntries(), func(files map[string][]byte) {
				good := sha256Hex(files["/releases/download/"+testVersion+"/"+name])
				files["/releases/download/"+testVersion+"/checksums.txt"] = []byte(tc.sums(good))
			})
			if err := os.MkdirAll(h.installDir, 0o755); err != nil {
				t.Fatal(err)
			}
			old := filepath.Join(h.installDir, "routerd.exe")
			if err := os.WriteFile(old, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
			expectFailure(t, out, err, tc.want)
			if got, _ := os.ReadFile(old); string(got) != "old" {
				t.Fatalf("existing routerd.exe modified: %q", got)
			}
			h.assertNoStaging()
		})
	}
}

func TestInstallPS1ArgumentValidation(t *testing.T) {
	for _, tc := range []struct {
		desc, arch string
		args       []string
		want       string
	}{
		{"unsupported arch", "x86", []string{"-Version", testVersion}, "unsupported architecture: x86"},
		{"invalid version", "AMD64", []string{"-Version", "1.2.3"}, "invalid version"},
		{"traversal version", "AMD64", []string{"-Version", "v1.2.3/../x"}, "invalid version"},
		{"http remote", "AMD64", []string{"-BaseUrl", "http://example.com", "-Version", testVersion}, "must use https"},
		{"http lookalike", "AMD64", []string{"-BaseUrl", "http://127.0.0.1.evil.example", "-Version", testVersion}, "must use https"},
		{"file scheme", "AMD64", []string{"-BaseUrl", "file:///C:/x", "-Version", testVersion}, "must use https"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			h := newPS1Harness(t, tc.arch)
			out, err := h.run(append([]string{"-InstallDir", h.installDir}, tc.args...)...)
			expectFailure(t, out, err, tc.want)
			if _, err := os.Stat(h.installDir); err == nil {
				t.Fatal("install dir must not be created")
			}
		})
	}
}

func TestInstallPS1DryRun(t *testing.T) {
	h := newPS1Harness(t, "AMD64")
	rel := newZipRelease(t, defaultZipEntries(), nil)
	out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion, "-DryRun")...)
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	if rel.requestCount() != 0 {
		t.Fatalf("dry run made %d requests", rel.requestCount())
	}
	if _, err := os.Stat(h.installDir); err == nil {
		t.Fatal("dry run must not create the install dir")
	}
}

func TestInstallPS1Cosign(t *testing.T) {
	t.Run("exact arguments", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		h.installFakeCosign(0)
		rel := newZipRelease(t, defaultZipEntries(), nil)
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		logged, err := os.ReadFile(h.cosignLog)
		if err != nil {
			t.Fatalf("cosign not invoked: %v", err)
		}
		got := strings.Fields(string(logged))
		want := []string{"verify-blob", "--bundle", "checksums.txt.sigstore.json",
			"--certificate-identity", "https://github.com/abyssbugg/ant-router/.github/workflows/release.yml@refs/tags/v1.2.3",
			"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com", "checksums.txt"}
		if len(got) != len(want) {
			t.Fatalf("cosign args = %q, want %q", got, want)
		}
		for i := range want {
			if strings.HasSuffix(want[i], ".json") || want[i] == "checksums.txt" {
				if filepath.Base(got[i]) != want[i] {
					t.Fatalf("cosign arg %d = %q, want path ending %q", i, got[i], want[i])
				}
				continue
			}
			if got[i] != want[i] {
				t.Fatalf("cosign arg %d = %q, want %q", i, got[i], want[i])
			}
		}
		h.assertInstalled()
	})
	t.Run("failure aborts", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		h.installFakeCosign(1)
		rel := newZipRelease(t, defaultZipEntries(), nil)
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		expectFailure(t, out, err, "signature verification failed")
		if _, err := os.Stat(filepath.Join(h.installDir, "routerd.exe")); err == nil {
			t.Fatal("nothing may be installed")
		}
	})
	t.Run("missing bundle aborts", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		h.installFakeCosign(0)
		rel := newZipRelease(t, defaultZipEntries(), func(f map[string][]byte) {
			delete(f, "/releases/download/"+testVersion+"/checksums.txt.sigstore.json")
		})
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		expectFailure(t, out, err, "checksums.txt.sigstore.json")
	})
	t.Run("NoVerifySignature skips cosign", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		h.installFakeCosign(1)
		rel := newZipRelease(t, defaultZipEntries(), nil)
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion, "-NoVerifySignature")...)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		if _, err := os.Stat(h.cosignLog); err == nil {
			t.Fatal("cosign must not run with -NoVerifySignature")
		}
		h.assertInstalled()
	})
}

func TestInstallPS1ExtractsOnlyBinaries(t *testing.T) {
	h := newPS1Harness(t, "AMD64")
	entries := append(defaultZipEntries(), zipEntry{"../escaped.txt", "x"}, zipEntry{`..\escaped2.txt`, "x"})
	rel := newZipRelease(t, entries, nil)
	out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	h.assertInstalled()
	for _, p := range []string{filepath.Join(h.dir, "escaped.txt"), filepath.Join(h.dir, "escaped2.txt")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("traversal entry written: %s", p)
		}
	}

	t.Run("missing binary", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		rel := newZipRelease(t, []zipEntry{{"routerd.exe", "d"}}, nil)
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		expectFailure(t, out, err, "failed to extract routerctl.exe")
	})
}
