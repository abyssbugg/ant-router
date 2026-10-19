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

// The Chocolatey community feed is an unauthenticated third-party service with
// recurring outages (HTTP 503); a CI gate must not depend on it.
func TestWindowsScriptDoesNotDependOnChocolatey(t *testing.T) {
	script := readWindowsScript(t)
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
	script := readWindowsScript(t)
	url := regexp.MustCompile(`'https://github\.com/niXman/mingw-builds-binaries/releases/download/[^']+\.7z'`)
	if !url.MatchString(script) {
		t.Fatal("windows.ps1 must download MinGW-w64 from a pinned GitHub release URL")
	}
	sha := regexp.MustCompile(`'[0-9a-f]{64}'`)
	if !sha.MatchString(script) {
		t.Fatal("windows.ps1 must pin the archive SHA-256")
	}
	for _, want := range []string{"Get-FileHash", "-Algorithm SHA256", "mingw32-make.exe"} {
		if !strings.Contains(script, want) {
			t.Fatalf("windows.ps1 must contain %q", want)
		}
	}
}
