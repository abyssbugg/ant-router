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
	{"manual redirects", regexp.MustCompile(`\$handler\.AllowAutoRedirect = \$false`)},
	{"every hop validated", regexp.MustCompile(`if \(-not \(Test-AllowedUri \$current\)\) \{`)},
	{"bounded request time", regexp.MustCompile(`\.Timeout = \[TimeSpan\]::FromMinutes\(10\)`)},
	{"userinfo rejected", regexp.MustCompile(`if \(\$parsed\.UserInfo\) \{ return \$false \}`)},
	{"two-argument ExtractToFile", regexp.MustCompile(`ExtractToFile\(\$entries\[0\], \(Join-Path \$extractDir \$bin\)\)`)},
	{"SHA-256 file hash", regexp.MustCompile(`Get-FileHash -LiteralPath \$archivePath -Algorithm SHA256`)},
	{"case-sensitive hash format", regexp.MustCompile(`-cnotmatch '\^\[0-9a-f\]\{64\}\$'`)},
	{"case-sensitive hash compare", regexp.MustCompile(`\$actual -cne \$expected`)},
	{"exact checksum filename", regexp.MustCompile(`\$name -ceq \$archive`)},
	{"cosign identity", regexp.MustCompile(regexp.QuoteMeta(`$CertIdentity = 'https://circleci.com/api/v2/projects/cc5f04e8-66e9-4f18-91b0-71c671674d14/pipeline-definitions/b37a0ec3-4a25-4686-b480-6b4eda16099f'`))},
	{"cosign issuer", regexp.MustCompile(regexp.QuoteMeta(`$OidcIssuer = 'https://oidc.circleci.com'`))},
	{"cosign verify-blob", regexp.MustCompile(`verify-blob`)},
	{"zip assembly loaded", regexp.MustCompile(`Add-Type -AssemblyName System\.IO\.Compression\.FileSystem`)},
	{"per-entry extraction", regexp.MustCompile(`ZipFileExtensions\]::ExtractToFile`)},
	{"https-only URLs", regexp.MustCompile(`\$parsed\.Scheme -ceq 'https'`)},
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
		"https://circleci.com/api/v2/projects/cc5f04e8-66e9-4f18-91b0-71c671674d14/pipeline-definitions/b37a0ec3-4a25-4686-b480-6b4eda16099f",
		"https://oidc.circleci.com",
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
	server    *httptest.Server
	files     map[string][]byte
	mu        sync.Mutex
	requests  []string
	redirects map[string]string
}

func newZipRelease(t *testing.T, entries []zipEntry, mutate func(map[string][]byte)) *zipRelease {
	t.Helper()
	r := &zipRelease{files: map[string][]byte{}, redirects: map[string]string{}}
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
		location, redirect := r.redirects[req.URL.Path]
		r.mu.Unlock()
		if redirect {
			http.Redirect(w, req, location, http.StatusFound)
			return
		}
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

func (r *zipRelease) redirect(path, location string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.redirects[path] = location
}

func (r *zipRelease) requestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// requestsTo counts the requests made for path.
func (r *zipRelease) requestsTo(path string) int {
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
	tmpDir     string
	pwshDir    string
	cosignLog  string
	arch       string
	env        map[string]string
	// prelude is PowerShell run in the session before install.ps1, e.g. to
	// define a function that shadows a cmdlet for fault injection.
	prelude string
}

func newPS1Harness(t *testing.T, processorArch string) *ps1Harness {
	t.Helper()
	pwsh := lookTool(t, "pwsh")
	dir := t.TempDir()
	h := &ps1Harness{
		t:          t,
		pwsh:       pwsh,
		dir:        dir,
		binDir:     filepath.Join(dir, "fakebin"),
		installDir: filepath.Join(dir, "install"),
		tmpDir:     filepath.Join(dir, "tmp"),
		pwshDir:    filepath.Dir(pwsh),
		cosignLog:  filepath.Join(dir, "cosign.log"),
		arch:       processorArch,
		env:        map[string]string{},
	}
	for _, d := range []string{h.binDir, h.tmpDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// pwsh's own directory (e.g. /opt/homebrew/bin) often also holds a real
	// cosign, so expose pwsh through an isolated directory instead.
	if runtime.GOOS != "windows" {
		h.pwshDir = filepath.Join(dir, "pwshbin")
		if err := os.Mkdir(h.pwshDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(pwsh, filepath.Join(h.pwshDir, "pwsh")); err != nil {
			t.Fatal(err)
		}
	}
	// Signature verification is mandatory, so every run gets a passing fake
	// cosign by default.
	h.installFakeCosign(0)
	return h
}

// removeFakeCosign exercises the missing-cosign path, skipping when a real
// cosign in the system directories on the harness PATH would be found.
func (h *ps1Harness) removeFakeCosign() {
	h.t.Helper()
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("cosign"); err == nil && strings.HasPrefix(strings.ToLower(p), strings.ToLower(os.Getenv("SystemRoot"))) {
			h.t.Skipf("system cosign at %s shadows the missing-cosign path", p)
		}
		if err := os.Remove(filepath.Join(h.binDir, "cosign.cmd")); err != nil {
			h.t.Fatal(err)
		}
		return
	}
	for _, p := range []string{"/usr/bin/cosign", "/bin/cosign"} {
		if _, err := os.Stat(p); err == nil {
			h.t.Skipf("system cosign at %s shadows the missing-cosign path", p)
		}
	}
	if err := os.Remove(filepath.Join(h.binDir, "cosign")); err != nil {
		h.t.Fatal(err)
	}
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

var psParamName = regexp.MustCompile(`^-[A-Za-z]+$`)

// psQuote renders s as a PowerShell single-quoted literal.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func (h *ps1Harness) run(args ...string) (string, error) {
	h.t.Helper()
	script := filepath.Join(repoRoot(h.t), "scripts", "install.ps1")
	cmdArgs := append([]string{"-NoProfile", "-NonInteractive", "-File", script}, args...)
	if h.prelude != "" {
		// Functions shadow cmdlets for the whole session, including the
		// invoked script, so faults are injected without test hooks in it.
		var b strings.Builder
		b.WriteString(h.prelude)
		b.WriteString("\n& " + psQuote(script))
		for _, a := range args {
			if psParamName.MatchString(a) {
				b.WriteString(" " + a)
			} else {
				b.WriteString(" " + psQuote(a))
			}
		}
		b.WriteString("\nexit $LASTEXITCODE")
		cmdArgs = []string{"-NoProfile", "-NonInteractive", "-Command", b.String()}
	}
	cmd := exec.Command(h.pwsh, cmdArgs...)
	var env []string
	for _, kv := range os.Environ() {
		k := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		switch k {
		case "PATH", "PROCESSOR_ARCHITECTURE", "PROCESSOR_ARCHITEW6432", "ANT_ROUTER_INSTALL_DIR",
			"ANT_ROUTER_INSECURE_SKIP_SIGNATURE", "TMPDIR", "TMP", "TEMP":
			continue
		}
		if _, overridden := h.env[k]; overridden {
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
		"PATH="+h.binDir+string(os.PathListSeparator)+h.pwshDir+string(os.PathListSeparator)+system,
		"PROCESSOR_ARCHITECTURE="+h.arch,
		// [IO.Path]::GetTempPath() honours these, so extraction happens
		// inside a directory the test can inspect.
		"TMPDIR="+h.tmpDir, "TMP="+h.tmpDir, "TEMP="+h.tmpDir,
	)
	for k, v := range h.env {
		if v != "" {
			env = append(env, k+"="+v)
		}
	}
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
	for _, pattern := range []string{".*.tmp.*", ".*.old.*"} {
		if m, _ := filepath.Glob(filepath.Join(h.installDir, pattern)); len(m) != 0 {
			h.t.Fatalf("staging or backup files left behind: %v", m)
		}
	}
	if entries, _ := os.ReadDir(h.tmpDir); len(entries) != 0 {
		h.t.Fatalf("temporary files left behind: %v", entries)
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
		{"http loopback userinfo", "AMD64", []string{"-BaseUrl", "http://localhost:80@evil.example", "-Version", testVersion}, "must use https"},
		{"https token userinfo", "AMD64", []string{"-BaseUrl", "https://token@github.com/abyssbugg/ant-router", "-Version", testVersion}, "must not contain credentials"},
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
		// One argument per line; paths may contain spaces.
		got := strings.Split(strings.TrimRight(strings.ReplaceAll(string(logged), "\r\n", "\n"), "\n"), "\n")
		want := []string{"verify-blob", "--bundle", "checksums.txt.sigstore.json",
			"--certificate-identity", "https://circleci.com/api/v2/projects/cc5f04e8-66e9-4f18-91b0-71c671674d14/pipeline-definitions/b37a0ec3-4a25-4686-b480-6b4eda16099f",
			"--certificate-oidc-issuer", "https://oidc.circleci.com", "checksums.txt"}
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
	for _, optOut := range []struct {
		desc string
		args []string
		env  map[string]string
	}{
		{"NoVerifySignature", []string{"-NoVerifySignature"}, nil},
		{"insecure env opt-out", nil, map[string]string{"ANT_ROUTER_INSECURE_SKIP_SIGNATURE": "1"}},
	} {
		t.Run(optOut.desc+" skips cosign with a warning", func(t *testing.T) {
			h := newPS1Harness(t, "AMD64")
			h.installFakeCosign(1)
			for k, v := range optOut.env {
				h.env[k] = v
			}
			rel := newZipRelease(t, defaultZipEntries(), nil)
			out, err := h.run(append(append(h.commonArgs(rel), "-Version", testVersion), optOut.args...)...)
			if err != nil {
				t.Fatalf("install failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "WARNING: signature verification is disabled") {
				t.Fatalf("skipping verification must warn:\n%s", out)
			}
			if _, err := os.Stat(h.cosignLog); err == nil {
				t.Fatal("cosign must not run when verification is disabled")
			}
			h.assertInstalled()
		})
	}
	t.Run("missing cosign fails before any download", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		h.removeFakeCosign()
		rel := newZipRelease(t, defaultZipEntries(), nil)
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		expectFailure(t, out, err, "cosign is required")
		if n := rel.requestCount(); n != 0 {
			t.Fatalf("no requests expected, got %d", n)
		}
	})
}

func TestInstallPS1Redirects(t *testing.T) {
	asset := "/releases/download/" + testVersion + "/" + windowsArchiveName("amd64")
	t.Run("loopback asset redirect is followed", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		rel := newZipRelease(t, defaultZipEntries(), func(f map[string][]byte) { f["/objects/archive"] = f[asset] })
		rel.redirect(asset, "/objects/archive")
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		if !rel.requested("/objects/archive") {
			t.Fatal("redirect target was not requested")
		}
		h.assertInstalled()
	})
	for _, tc := range []struct{ desc, path, location string }{
		{"asset to remote http", asset, "http://example.invalid/archive"},
		{"asset to userinfo lookalike", asset, "http://localhost:80@example.invalid/archive"},
		{"checksums to remote http", "/releases/download/" + testVersion + "/checksums.txt", "http://example.invalid/checksums.txt"},
		{"latest to remote http", "/releases/latest", "http://example.invalid/releases/tag/" + testVersion},
	} {
		t.Run(tc.desc+" is rejected", func(t *testing.T) {
			h := newPS1Harness(t, "AMD64")
			rel := newZipRelease(t, defaultZipEntries(), nil)
			rel.redirect(tc.path, tc.location)
			args := h.commonArgs(rel)
			if tc.path != "/releases/latest" {
				args = append(args, "-Version", testVersion)
			}
			out, err := h.run(args...)
			expectFailure(t, out, err, "refusing to download")
			if _, err := os.Stat(filepath.Join(h.installDir, "routerd.exe")); err == nil {
				t.Fatal("nothing may be installed")
			}
		})
	}
	t.Run("redirect loop is bounded", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		rel := newZipRelease(t, defaultZipEntries(), nil)
		rel.redirect(asset, asset)
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		expectFailure(t, out, err, "too many redirects")
		// The first request plus at most 10 followed redirects.
		if n := rel.requestsTo(asset); n != 11 {
			t.Fatalf("archive requested %d times, want exactly 11", n)
		}
	})
}

func TestInstallPS1RejectsDirectoryDestination(t *testing.T) {
	h := newPS1Harness(t, "AMD64")
	rel := newZipRelease(t, defaultZipEntries(), nil)
	if err := os.MkdirAll(filepath.Join(h.installDir, "routerctl.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
	expectFailure(t, out, err, filepath.Join(h.installDir, "routerctl.exe")+" is a directory")
	if _, err := os.Stat(filepath.Join(h.installDir, "routerd.exe")); err == nil {
		t.Fatal("routerd.exe must not be installed when routerctl.exe cannot be")
	}
	h.assertNoStaging()
}

// TestInstallPS1RollsBackOnPartialFailure makes routerctl.exe impossible to
// move aside (BSD immutable flag) after routerd.exe has been replaced, and
// checks that the previous routerd.exe is restored.
// failStagedMove shadows Move-Item so that moving the staged routerctl.exe
// into place fails after routerd.exe was already replaced. The same
// injection works on every OS, unlike file-locking or immutable flags.
const failStagedMove = `function global:Move-Item {
    [CmdletBinding()]
    param([string]$LiteralPath, [string]$Destination, [switch]$Force)
    if ($LiteralPath -like '*.routerctl.exe.tmp.*') { throw 'injected move failure' }
    Microsoft.PowerShell.Management\Move-Item @PSBoundParameters
}`

func TestInstallPS1RollsBackOnPartialFailure(t *testing.T) {
	t.Run("upgrade restores the previous pair", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		rel := newZipRelease(t, defaultZipEntries(), nil)
		if err := os.MkdirAll(h.installDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, bin := range []string{"routerd.exe", "routerctl.exe"} {
			if err := os.WriteFile(filepath.Join(h.installDir, bin), []byte("old "+bin), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		h.prelude = failStagedMove
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		expectFailure(t, out, err, "failed to install binaries: injected move failure")
		for _, bin := range []string{"routerd.exe", "routerctl.exe"} {
			got, err := os.ReadFile(filepath.Join(h.installDir, bin))
			if err != nil || string(got) != "old "+bin {
				t.Fatalf("%s = %q (%v), want restored %q", bin, got, err, "old "+bin)
			}
		}
		h.assertNoStaging()
	})
	t.Run("fresh install leaves nothing behind", func(t *testing.T) {
		h := newPS1Harness(t, "AMD64")
		rel := newZipRelease(t, defaultZipEntries(), nil)
		h.prelude = failStagedMove
		out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
		expectFailure(t, out, err, "failed to install binaries: injected move failure")
		for _, bin := range []string{"routerd.exe", "routerctl.exe"} {
			if _, err := os.Stat(filepath.Join(h.installDir, bin)); !os.IsNotExist(err) {
				t.Fatalf("%s must not exist after a failed fresh install (err=%v)", bin, err)
			}
		}
		h.assertNoStaging()
	})
}

func TestInstallPS1ExtractsOnlyBinaries(t *testing.T) {
	h := newPS1Harness(t, "AMD64")
	// Extraction happens in $TMPDIR/<random>/extract, so ../../ entries would
	// land directly in the harness TMPDIR.
	entries := append(defaultZipEntries(), zipEntry{"../../escaped.txt", "x"}, zipEntry{`..\..\escaped2.txt`, "x"})
	rel := newZipRelease(t, entries, nil)
	out, err := h.run(append(h.commonArgs(rel), "-Version", testVersion)...)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	h.assertInstalled()
	for _, p := range []string{filepath.Join(h.tmpDir, "escaped.txt"), filepath.Join(h.tmpDir, "escaped2.txt"), filepath.Join(h.dir, "escaped.txt")} {
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
