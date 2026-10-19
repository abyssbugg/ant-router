package ci_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readWindowsScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".circleci", "scripts", "windows.ps1"))
	if err != nil {
		t.Fatalf("read windows.ps1: %v", err)
	}
	return string(data)
}

// blockComment matches PowerShell <# ... #> comments, which document rather
// than execute.
var blockComment = regexp.MustCompile(`(?s)<#.*?#>`)

// The Chocolatey community feed is an unauthenticated third-party service with
// recurring outages (HTTP 503); a CI gate must not depend on it.
func TestWindowsScriptDoesNotDependOnChocolatey(t *testing.T) {
	script := blockComment.ReplaceAllString(readWindowsScript(t), "")
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), "choco") {
			t.Fatalf("windows.ps1 must not invoke Chocolatey: %q", trimmed)
		}
	}
}

func TestWindowsScriptPinsAndVerifiesToolchainArchive(t *testing.T) {
	script := blockComment.ReplaceAllString(readWindowsScript(t), "")
	// Exact release and asset: changing either must come with a new SHA-256.
	url := regexp.MustCompile(`(?m)^\$mingwUrl\s*=\s*'https://github\.com/niXman/mingw-builds-binaries/releases/download/13\.2\.0-rt_v11-rev1/x86_64-13\.2\.0-release-posix-seh-ucrt-rt_v11-rev1\.7z'\s*$`)
	if !url.MatchString(script) {
		t.Fatal("windows.ps1 must download MinGW-w64 from a pinned GitHub release URL")
	}
	sha := regexp.MustCompile(`(?m)^\$mingwSha256\s*=\s*'[0-9a-f]{64}'\s*$`)
	if !sha.MatchString(script) {
		t.Fatal("windows.ps1 must pin the archive SHA-256 in $mingwSha256")
	}
	// The pin is only meaningful if the computed hash is compared against it
	// and a mismatch aborts the job.
	hashCheck := regexp.MustCompile(`(?s)\$actualSha256\s*=\s*\(Get-FileHash\s+-LiteralPath\s+\$mingwArchive\s+-Algorithm\s+SHA256\)\.Hash\.ToLowerInvariant\(\)\s+if\s*\(\$actualSha256\s+-ne\s+\$mingwSha256\)\s*\{\s*throw\b`)
	if !hashCheck.MatchString(script) {
		t.Fatal("windows.ps1 must compare the computed archive SHA-256 and throw on mismatch")
	}
	extract := regexp.MustCompile(`(?m)^Invoke-Native \$sevenZip @\('x'`)
	extractAt := extract.FindStringIndex(script)
	if extractAt == nil {
		t.Fatal("windows.ps1 must extract the archive with 7-Zip")
	}
	if hashAt := hashCheck.FindStringIndex(script); hashAt[0] > extractAt[0] {
		t.Fatal("windows.ps1 must verify the archive SHA-256 before extracting it")
	}
	// The bin directory must be the one inside the verified, extracted archive.
	extractTarget := regexp.MustCompile(`(?m)^Invoke-Native \$sevenZip @\('x',[^\n]*"-o\$toolsDir",\s*\$mingwArchive\)\s*$`)
	if !extractTarget.MatchString(script) {
		t.Fatal("windows.ps1 must extract the verified archive into $toolsDir")
	}
	binResolve := regexp.MustCompile(`(?m)^\$mingwBin\s*=\s*Join-Path\s+\$toolsDir\s+'mingw64\\bin'\s*$`)
	if !binResolve.MatchString(script) {
		t.Fatal("windows.ps1 must resolve $mingwBin to mingw64\\bin inside the extracted archive")
	}
	makeResolve := regexp.MustCompile(`(?m)^\$make\s*=\s*Join-Path\s+\$mingwBin\s+'mingw32-make\.exe'\s*$`)
	if !makeResolve.MatchString(script) {
		t.Fatal("windows.ps1 must resolve mingw32-make.exe from the pinned MinGW bin directory")
	}
}
