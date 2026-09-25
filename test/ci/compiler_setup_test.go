package ci_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func windowsCoverageScript(t *testing.T) string {
	t.Helper()
	return readWindowsCoverageScript(t, filepath.Join("..", "..", ".circleci", "scripts", "pr-coverage-windows.ps1"))
}

func TestWindowsCoverageScriptLineEndings(t *testing.T) {
	const normalized = "choco upgrade mingw\nif ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }\nWrite-Host $version\n"
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("ending-%q", ending), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tmp_rovodev_checkout.ps1")
			if err := os.WriteFile(path, []byte(strings.ReplaceAll(normalized, "\n", ending)), 0600); err != nil {
				t.Fatal(err)
			}
			if got := readWindowsCoverageScript(t, path); got != normalized {
				t.Fatalf("checkout not normalized: got %q", got)
			}
		})
	}
}

func readWindowsCoverageScript(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func TestWindowsCompilerSetupMutations(t *testing.T) {
	script := windowsCoverageScript(t)
	if err := validateCoverageScript("windows", script); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"choco upgrade mingw --version=13.2.0 --source=https://community.chocolatey.org/api/v2/ --yes --no-progress --require-checksums --allow-downgrade --install-if-not-installed --execution-timeout=180",
		"$gccVersion = gcc -dumpfullversion",
		"$gccTarget = gcc -dumpmachine",
		"$raceLibrary = gcc -print-file-name=libsynchronization.a",
	} {
		block := statement + "\n" + nativeExitCheck
		if strings.Count(script, block) != 1 {
			t.Fatalf("expected one checked native command: %s", statement)
		}
		for _, mode := range []string{"unchecked", "commented", "moved"} {
			t.Run(statement+"/"+mode, func(t *testing.T) {
				var mutated string
				switch mode {
				case "unchecked":
					mutated = strings.Replace(script, block, statement, 1)
				case "commented":
					mutated = strings.Replace(script, block, "# "+statement+"\n"+nativeExitCheck, 1)
				case "moved":
					mutated = strings.Replace(script, block, "", 1) + "\n" + block + "\n"
				}
				if err := validateCoverageScript("windows", mutated); err == nil {
					t.Fatal("accepted weakened compiler setup")
				}
			})
		}
	}
}

func TestWindowsCompilerSetupBehavior(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires native Windows PowerShell; local contract tests do not prove installation")
	}
	script := windowsCoverageScript(t)
	start := strings.Index(script, "$null = Get-Command choco -ErrorAction Stop")
	end := strings.Index(script, "Write-Host $version\n")
	if start < 0 || end <= start {
		t.Fatal("missing compiler setup block")
	}
	block := script[start:end]
	for _, tc := range []struct {
		mode string
		code int
		want string
	}{
		{"pass", 0, "compiler-fixture-passed"},
		{"install-failed", 42, "installer-fixture"},
		{"reboot-required", 3010, "installer-fixture"},
		{"missing-compiler", 1, "Pinned MinGW compiler missing after installation"},
		{"version-failed", 43, "version-fixture"},
		{"wrong-version", 1, "Pinned MinGW version mismatch"},
		{"target-failed", 44, "target-fixture"},
		{"wrong-target", 1, "MinGW target must be Windows amd64"},
		{"library-failed", 45, "library-fixture"},
		{"old-runtime", 1, "MinGW runtime lacks Go race support"},
		{"missing-library", 1, "MinGW runtime lacks Go race support"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "compiler fixture")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"gcc.exe", "libsynchronization.a"} {
				if (tc.mode == "missing-compiler" && name == "gcc.exe") || (tc.mode == "missing-library" && name == "libsynchronization.a") {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Only relocate the package directory. Execute the real guards and
			// PATH update with isolated installer/compiler functions, never choco
			// or a real compiler. Native executable calls set LASTEXITCODE; these
			// boundary stubs supply controlled results without machine mutations.
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
			harness := "$ErrorActionPreference = 'Stop'\n$mode = " + quote(tc.mode) + "\n$fixture = " + quote(dir) + "\n" + compilerBoundaryFixtures
			body := strings.Replace(block, "'C:\\ProgramData\\mingw64\\mingw64\\bin'", quote(dir), 1)
			if body == block {
				t.Fatal("fixture did not relocate installation path")
			}
			harness += body + "\nWrite-Host 'compiler-fixture-passed'\n"
			file := filepath.Join(dir, "tmp_rovodev_compiler_fixture.ps1")
			if err := os.WriteFile(file, []byte(harness), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", file).CombinedOutput()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatalf("PowerShell invocation: %v", err)
				}
				code = exit.ExitCode()
			}
			if ctx.Err() != nil || code != tc.code || !strings.Contains(string(out), tc.want) {
				t.Fatalf("mode=%s exit=%d want=%d err=%v output=%s", tc.mode, code, tc.code, err, out)
			}
			if tc.code != 0 && strings.Contains(string(out), "compiler-fixture-passed") {
				t.Fatal("compiler setup continued after failure")
			}
		})
	}
}

const compilerBoundaryFixtures = `
function choco {
    if (($args -join ' ') -ne 'upgrade mingw --version=13.2.0 --source=https://community.chocolatey.org/api/v2/ --yes --no-progress --require-checksums --allow-downgrade --install-if-not-installed --execution-timeout=180') { throw 'Unexpected installer arguments' }
    Write-Host 'installer-fixture'
    $global:LASTEXITCODE = 0
    if ($mode -eq 'install-failed') { $global:LASTEXITCODE = 42 }
    if ($mode -eq 'reboot-required') { $global:LASTEXITCODE = 3010 }
}
function gcc {
    if (-not $env:PATH.StartsWith($fixture + ';')) { throw 'Compiler directory not prepended to process PATH' }
    $global:LASTEXITCODE = 0
    switch ($args[0]) {
        '-dumpfullversion' {
            Write-Host 'version-fixture'
            if ($mode -eq 'version-failed') { $global:LASTEXITCODE = 43 }
            if ($mode -eq 'wrong-version') { '12.0.0' } else { '13.2.0' }
        }
        '-dumpmachine' {
            Write-Host 'target-fixture'
            if ($mode -eq 'target-failed') { $global:LASTEXITCODE = 44 }
            if ($mode -eq 'wrong-target') { 'i686-w64-mingw32' } else { 'x86_64-w64-mingw32' }
        }
        '-print-file-name=libsynchronization.a' {
            Write-Host 'library-fixture'
            if ($mode -eq 'library-failed') { $global:LASTEXITCODE = 45 }
            if ($mode -eq 'old-runtime') { 'libsynchronization.a' } else { Join-Path $fixture 'libsynchronization.a' }
        }
        default { throw 'Unexpected compiler arguments' }
    }
}
`
