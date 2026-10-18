package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderLaunchdPlistBasic tests basic plist rendering.
func TestRenderLaunchdPlistBasic(t *testing.T) {
	spec := Spec{
		Label:      "com.example.test",
		Executable: "/usr/bin/test",
		Args:       []string{"arg1", "arg2"},
		LogPath:    "/var/log/test.log",
	}

	// Create the executable for validation
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "test")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	spec.Executable = exe

	plist, err := renderLaunchdPlist(spec)
	if err != nil {
		t.Fatalf("renderLaunchdPlist: %v", err)
	}

	if !bytes.Contains(plist, []byte(OwnershipMarker)) {
		t.Error("plist missing ownership marker")
	}
	if !bytes.Contains(plist, []byte("com.example.test")) {
		t.Error("plist missing label")
	}
	if !bytes.Contains(plist, []byte(exe)) {
		t.Error("plist missing executable")
	}
	if !bytes.Contains(plist, []byte("arg1")) {
		t.Error("plist missing arg1")
	}
	if !bytes.Contains(plist, []byte("<true/>")) {
		t.Error("plist missing RunAtLoad")
	}
	if !bytes.Contains(plist, []byte("<false/>")) {
		t.Error("plist missing KeepAlive SuccessfulExit false")
	}
}

// TestRenderLaunchdPlistGolden tests plist rendering against golden file.
func TestRenderLaunchdPlistGolden(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "myapp")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "com.company.myapp",
		Executable: exe,
		Args:       nil,
		LogPath:    logPath,
	}

	plist, err := renderLaunchdPlist(spec)
	if err != nil {
		t.Fatalf("renderLaunchdPlist: %v", err)
	}

	// Verify it's valid XML by checking structure
	if !strings.HasPrefix(string(plist), "<?xml") {
		t.Fatal("plist missing XML declaration")
	}
	if !strings.Contains(string(plist), "<dict>") || !strings.Contains(string(plist), "</dict>") {
		t.Fatal("plist missing dict structure")
	}

	assertGoldenPlist(t, "launchd_basic.plist", plist, exe, logPath)
}

// TestRenderLaunchdPlistEscaping tests XML escaping of special characters.
func TestRenderLaunchdPlistEscaping(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "test")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "log.txt")

	spec := Spec{
		Label:      "com.test.app.valid",
		Executable: exe,
		Args: []string{
			"--config=/path&file<special>.xml",
			"--data=value>less",
		},
		LogPath: logPath,
	}

	plist, err := renderLaunchdPlist(spec)
	if err != nil {
		t.Fatalf("renderLaunchdPlist: %v", err)
	}

	// Verify it's valid XML by checking structure
	if !strings.HasPrefix(string(plist), "<?xml") {
		t.Fatal("plist missing XML declaration")
	}

	// Verify escaped sequences are present
	if !bytes.Contains(plist, []byte("&amp;")) {
		t.Error("& not escaped")
	}
	if !bytes.Contains(plist, []byte("&lt;")) {
		t.Error("< not escaped")
	}
	if !bytes.Contains(plist, []byte("&gt;")) {
		t.Error("> not escaped")
	}
}

// TestSpecValidateControlCharacterInArg tests that control characters are rejected in args.
func TestSpecValidateControlCharacterInArg(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		Args:       []string{"arg1", "arg\x00with\x1fnul"},
		LogPath:    logPath,
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for control character in arg, got nil")
	}
}

// TestSpecValidateControlCharacterInLogPath tests that control characters are rejected in logpath.
func TestSpecValidateControlCharacterInLogPath(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		Args:       []string{},
		LogPath:    filepath.Join(tmpDir, "log\x00.txt"),
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for control character in logpath, got nil")
	}
}

// TestSpecValidation tests spec validation.
func TestSpecValidation(t *testing.T) {
	tests := []struct {
		name    string
		spec    Spec
		wantErr string
	}{
		{
			name: "relative executable",
			spec: Spec{
				Label:      "test",
				Executable: "relative/path",
				LogPath:    "/abs/log",
			},
			wantErr: "path must be absolute",
		},
		{
			name: "missing executable",
			spec: Spec{
				Label:      "test",
				Executable: "/nonexistent/exe",
				LogPath:    "/abs/log",
			},
			wantErr: "is not a regular file",
		},
		{
			name: "relative log path",
			spec: Spec{
				Label:      "test",
				Executable: "/bin/sh",
				LogPath:    "relative/log",
			},
			wantErr: "path must be absolute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if err == nil {
				t.Fatalf("Validate: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate: got %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateLabelTraversal tests that labels with traversal sequences are rejected.
func TestValidateLabelTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "../../evil",
		Executable: exe,
		LogPath:    logPath,
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for traversal label, got nil")
	}
	if !strings.Contains(err.Error(), "invalid label") {
		t.Errorf("got %q, want substring 'invalid label'", err.Error())
	}
}

// TestValidateLabelSlash tests that labels with slashes are rejected.
func TestValidateLabelSlash(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "com/test",
		Executable: exe,
		LogPath:    logPath,
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for slash label, got nil")
	}
	if !strings.Contains(err.Error(), "invalid label") {
		t.Errorf("got %q, want substring 'invalid label'", err.Error())
	}
}

// TestValidateLabelEmpty tests that empty labels are rejected.
func TestValidateLabelEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "",
		Executable: exe,
		LogPath:    logPath,
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for empty label, got nil")
	}
	if !strings.Contains(err.Error(), "invalid label") {
		t.Errorf("got %q, want substring 'invalid label'", err.Error())
	}
}

// TestValidateLabelLeadingDot tests that labels starting with dot are rejected.
func TestValidateLabelLeadingDot(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      ".hidden",
		Executable: exe,
		LogPath:    logPath,
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for leading dot label, got nil")
	}
	if !strings.Contains(err.Error(), "invalid label") {
		t.Errorf("got %q, want substring 'invalid label'", err.Error())
	}
}

// TestValidateLabelSpaces tests that labels with spaces are rejected.
func TestValidateLabelSpaces(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "com test app",
		Executable: exe,
		LogPath:    logPath,
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for label with spaces, got nil")
	}
	if !strings.Contains(err.Error(), "invalid label") {
		t.Errorf("got %q, want substring 'invalid label'", err.Error())
	}
}

// TestValidateLabelUnicode tests that labels with unicode are rejected.
func TestValidateLabelUnicode(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "com.test.café",
		Executable: exe,
		LogPath:    logPath,
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for unicode label, got nil")
	}
	if !strings.Contains(err.Error(), "invalid label") {
		t.Errorf("got %q, want substring 'invalid label'", err.Error())
	}
}

// TestValidateLabelTooLong tests that labels >128 chars are rejected.
func TestValidateLabelTooLong(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "com." + strings.Repeat("a", 130),
		Executable: exe,
		LogPath:    logPath,
	}
	err := spec.Validate()
	if err == nil {
		t.Fatalf("Validate: expected error for too-long label, got nil")
	}
	if !strings.Contains(err.Error(), "invalid label") {
		t.Errorf("got %q, want substring 'invalid label'", err.Error())
	}
}

// TestExecRunnerBoundedOutput tests that ExecRunner caps output at 64 KiB.
func TestExecRunnerBoundedOutput(t *testing.T) {
	runner := &ExecRunner{}
	ctx := context.Background()

	// Run a command that outputs >64KiB (if possible)
	// Use dd to generate large output
	stdout, stderr, err := runner.Run(ctx, "sh", "-c", "dd if=/dev/zero bs=1024 count=100 2>/dev/null | head -c 100000")
	_ = stderr
	if err != nil {
		// Some systems may not have dd; that's OK for this test
		t.Skip("sh/dd not available")
	}

	if len(stdout) > maxOutputSize {
		t.Errorf("stdout exceeds limit: %d > %d", len(stdout), maxOutputSize)
	}
}

// TestFakeRunnerInstall tests Install with a fake runner.
func TestFakeRunnerInstall(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "app.log")
	homeDir := tmpDir

	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		Args:       []string{"-v"},
		LogPath:    logPath,
	}

	fake := newFakeRunner()
	backend := NewLaunchdBackend(homeDir, 501, fake)

	ctx := context.Background()
	err := backend.Install(ctx, spec)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify bootout was called (may fail, so we ignore error)
	// Verify bootstrap was called
	bootstrapCall := findCall(fake.calls, "launchctl", "bootstrap")
	if bootstrapCall == nil {
		t.Error("bootstrap not called")
	}

	// Verify plist was written
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "com.test.app.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	if !bytes.Contains(data, []byte(OwnershipMarker)) {
		t.Error("plist missing ownership marker")
	}
}

// TestFakeRunnerUninstall tests Uninstall refuses unowned files.
func TestFakeRunnerUninstall(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := tmpDir

	// Create an unowned plist
	agentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	os.MkdirAll(agentsDir, 0755)
	unownedPath := filepath.Join(agentsDir, "com.test.other.plist")
	os.WriteFile(unownedPath, []byte("<?xml version=\"1.0\"?><plist></plist>"), 0644)

	fake := newFakeRunner()
	backend := NewLaunchdBackend(homeDir, 501, fake)

	ctx := context.Background()
	err := backend.Uninstall(ctx, "com.test.other")

	var notOwnedErr ErrNotOwned
	if !isErrNotOwned(err) {
		t.Fatalf("Uninstall: expected ErrNotOwned, got %v", err)
	}
	_ = notOwnedErr
}

// TestUninstallNotLoadedIgnoresError tests that uninstall ignores "not loaded" bootout errors.
func TestUninstallNotLoadedIgnoresError(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := tmpDir
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	agentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	os.MkdirAll(agentsDir, 0755)
	plistPath := filepath.Join(agentsDir, "com.test.app.plist")
	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		LogPath:    logPath,
	}
	plist, _ := renderLaunchdPlist(spec)
	os.WriteFile(plistPath, plist, 0644)

	fake := newFakeRunner()
	fake.errMap = map[string]error{"bootout": fmt.Errorf("bootout failed")}
	fake.stderrMap = map[string][]byte{"bootout": []byte("Could not find service")}

	backend := NewLaunchdBackend(homeDir, 501, fake)
	ctx := context.Background()
	err := backend.Uninstall(ctx, "com.test.app")
	if err != nil {
		t.Errorf("Uninstall: expected nil for not-loaded error, got %v", err)
	}

	if _, err := os.Stat(plistPath); err == nil {
		t.Error("plist file should be deleted")
	}
}

// TestUninstallBootoutErrorKeepsFile tests that uninstall keeps file on other bootout errors.
func TestUninstallBootoutErrorKeepsFile(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := tmpDir
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	agentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	os.MkdirAll(agentsDir, 0755)
	plistPath := filepath.Join(agentsDir, "com.test.app.plist")
	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		LogPath:    logPath,
	}
	plist, _ := renderLaunchdPlist(spec)
	os.WriteFile(plistPath, plist, 0644)

	fake := newFakeRunner()
	fake.errMap = map[string]error{"bootout": fmt.Errorf("permission denied")}
	fake.stderrMap = map[string][]byte{"bootout": []byte("Permission denied")}

	backend := NewLaunchdBackend(homeDir, 501, fake)
	ctx := context.Background()
	err := backend.Uninstall(ctx, "com.test.app")
	if err == nil {
		t.Error("Uninstall: expected error for permission denied, got nil")
	}

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		t.Error("plist file should be kept on error")
	}
}

// TestStatusParsing tests status parsing for running/stopped/not-found.
func TestStatusParsing(t *testing.T) {
	tests := []struct {
		name          string
		launchctlOut  string
		expectedState State
	}{
		{
			name:          "running",
			launchctlOut:  "state = running\n",
			expectedState: StateRunning,
		},
		{
			name:          "stopped",
			launchctlOut:  "state = idle\n",
			expectedState: StateStopped,
		},
		{
			name:          "not-found",
			launchctlOut:  "",
			expectedState: StateNotInstalled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			fake := &fakeRunner{
				stdoutMap: map[string][]byte{
					"print": []byte(tt.launchctlOut),
				},
			}
			if tt.name == "not-found" {
				fake.errMap = map[string]error{
					"print": errNotFound,
				}
			}

			backend := NewLaunchdBackend(tmpDir, 501, fake)
			ctx := context.Background()

			status, err := backend.Status(ctx, "com.test.app")
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if status.State != tt.expectedState {
				t.Errorf("Status: got state %v, want %v", status.State, tt.expectedState)
			}
		})
	}
}

// Helpers

func assertGoldenPlist(t *testing.T, name string, plist []byte, exe, logPath string) {
	t.Helper()
	path := filepath.Join("testdata", name)

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		// Replace exe and logPath with placeholders for reproducibility
		golden := bytes.ReplaceAll(plist, []byte(exe), []byte("$EXE"))
		golden = bytes.ReplaceAll(golden, []byte(logPath), []byte("$LOG"))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, golden, 0o644); err != nil {
			t.Fatalf("WriteFile golden: %v", err)
		}
	}

	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile golden: %v", err)
	}

	// Replace placeholders with actual paths for comparison
	golden = bytes.ReplaceAll(golden, []byte("$EXE"), []byte(exe))
	golden = bytes.ReplaceAll(golden, []byte("$LOG"), []byte(logPath))

	if !bytes.Equal(golden, plist) {
		t.Errorf("plist differs from golden; run UPDATE_GOLDEN=1 to update")
	}
}

type fakeRunner struct {
	calls     [][]string
	stdoutMap map[string][]byte
	stderrMap map[string][]byte
	errMap    map[string]error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		calls:     [][]string{},
		stdoutMap: make(map[string][]byte),
		stderrMap: make(map[string][]byte),
		errMap:    make(map[string]error),
	}
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)

	// Check for configured error/output
	if len(args) > 0 {
		key := args[0]
		if e, ok := f.errMap[key]; ok {
			stderrOut := []byte("Could not find service")
			if s, ok := f.stderrMap[key]; ok {
				stderrOut = s
			}
			return nil, stderrOut, e
		}
		if out, ok := f.stdoutMap[key]; ok {
			return out, nil, nil
		}
	}

	return nil, nil, nil
}

func findCall(calls [][]string, name, arg string) []string {
	for _, call := range calls {
		if len(call) > 0 && call[0] == name && len(call) > 1 && call[1] == arg {
			return call
		}
	}
	return nil
}

func isErrNotOwned(err error) bool {
	_, ok := err.(ErrNotOwned)
	return ok
}

var errNotFound = &mockError{msg: "service not found"}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

func TestLaunchdBackendRejectsUnsafeLabelsBeforeAnySideEffect(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "routerd")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"../../escape", "a/b", "", ".hidden", "has space"} {
		fake := newFakeRunner()
		backend := NewLaunchdBackend(home, 501, fake)
		ctx := context.Background()
		spec := Spec{Label: label, Executable: executable, LogPath: filepath.Join(root, "routerd.log")}
		_, statusErr := backend.Status(ctx, label)
		for name, err := range map[string]error{
			"install":   backend.Install(ctx, spec),
			"uninstall": backend.Uninstall(ctx, label),
			"start":     backend.Start(ctx, label),
			"stop":      backend.Stop(ctx, label),
			"status":    statusErr,
		} {
			var invalid ErrInvalidLabel
			if !errors.As(err, &invalid) {
				t.Errorf("%s(%q) error = %v, want ErrInvalidLabel", name, label, err)
			}
		}
		if len(fake.calls) != 0 {
			t.Errorf("label %q reached launchctl: %v", label, fake.calls)
		}
	}
	var created []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && filepath.Ext(path) == ".plist" {
			created = append(created, path)
		}
		return nil
	})
	if len(created) != 0 {
		t.Fatalf("unsafe labels created plists: %v", created)
	}
}

// TestLaunchdInstallRefusesUnownedPlist verifies Install refuses to overwrite unowned plists.
func TestLaunchdInstallRefusesUnownedPlist(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile exe: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	// Create an unowned plist at the target path
	agentsDir := filepath.Join(tmpDir, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	plistPath := filepath.Join(agentsDir, "com.test.app.plist")
	unownedContent := []byte("<?xml version=\"1.0\"?><plist><dict><key>Label</key><string>com.test.app</string></dict></plist>")
	if err := os.WriteFile(plistPath, unownedContent, 0644); err != nil {
		t.Fatalf("WriteFile unowned: %v", err)
	}

	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		Args:       []string{},
		LogPath:    logPath,
	}

	fake := newFakeRunner()
	backend := NewLaunchdBackend(tmpDir, 501, fake)

	ctx := context.Background()
	err := backend.Install(ctx, spec)

	// Should return ErrNotOwned
	if !isErrNotOwned(err) {
		t.Fatalf("Install: expected ErrNotOwned, got %v", err)
	}

	// Plist should be unchanged (byte-identical)
	updated, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("ReadFile plist after Install: %v", err)
	}
	if !bytes.Equal(updated, unownedContent) {
		t.Errorf("plist was modified: expected %q, got %q", unownedContent, updated)
	}

	// No bootstrap should have been called
	bootstrapCall := findCall(fake.calls, "launchctl", "bootstrap")
	if bootstrapCall != nil {
		t.Error("bootstrap was called despite refusing Install")
	}
}

// TestLaunchdStartRefusesUnownedAndIssuesNoCommand verifies Start refuses unowned plists.
func TestLaunchdStartRefusesUnownedAndIssuesNoCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an unowned plist
	agentsDir := filepath.Join(tmpDir, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	plistPath := filepath.Join(agentsDir, "com.test.app.plist")
	if err := os.WriteFile(plistPath, []byte("<?xml version=\"1.0\"?><plist><dict></dict></plist>"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fake := newFakeRunner()
	backend := NewLaunchdBackend(tmpDir, 501, fake)

	ctx := context.Background()
	err := backend.Start(ctx, "com.test.app")

	// Should return ErrNotOwned
	if !isErrNotOwned(err) {
		t.Fatalf("Start: expected ErrNotOwned, got %v", err)
	}

	// No launchctl commands should have been recorded
	if len(fake.calls) != 0 {
		t.Errorf("Start issued commands despite refusing: %v", fake.calls)
	}
}

// TestLaunchdStopRefusesUnownedAndIssuesNoCommand verifies Stop refuses unowned plists.
func TestLaunchdStopRefusesUnownedAndIssuesNoCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an unowned plist
	agentsDir := filepath.Join(tmpDir, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	plistPath := filepath.Join(agentsDir, "com.test.app.plist")
	if err := os.WriteFile(plistPath, []byte("<?xml version=\"1.0\"?><plist><dict></dict></plist>"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fake := newFakeRunner()
	backend := NewLaunchdBackend(tmpDir, 501, fake)

	ctx := context.Background()
	err := backend.Stop(ctx, "com.test.app")

	// Should return ErrNotOwned
	if !isErrNotOwned(err) {
		t.Fatalf("Stop: expected ErrNotOwned, got %v", err)
	}

	// No launchctl commands should have been recorded
	if len(fake.calls) != 0 {
		t.Errorf("Stop issued commands despite refusing: %v", fake.calls)
	}
}

// TestLaunchdInstallPropagatesBootoutError verifies real bootout errors are propagated.
func TestLaunchdInstallPropagatesBootoutError(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		Args:       []string{},
		LogPath:    logPath,
	}

	// Mock runner that returns a real bootout error (not "not loaded")
	fake := newFakeRunner()
	fake.errMap = map[string]error{"bootout": fmt.Errorf("permission denied")}
	fake.stderrMap = map[string][]byte{"bootout": []byte("permission denied")}

	backend := NewLaunchdBackend(tmpDir, 501, fake)

	ctx := context.Background()
	err := backend.Install(ctx, spec)

	// Should return an error (not silently ignore)
	if err == nil {
		t.Fatal("Install: expected error for bootout failure, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("Install: error should mention bootout failure, got %v", err)
	}
}

// TestLaunchdInstallCreatesLogDirectory verifies log directory is created.
func TestLaunchdInstallCreatesLogDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Specify a log path in a non-existent subdirectory
	logPath := filepath.Join(tmpDir, "logs", "subdir", "app.log")

	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		Args:       []string{},
		LogPath:    logPath,
	}

	fake := newFakeRunner()
	backend := NewLaunchdBackend(tmpDir, 501, fake)

	ctx := context.Background()
	err := backend.Install(ctx, spec)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Log directory should exist
	logDir := filepath.Dir(logPath)
	info, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("log directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("log path is not a directory")
	}
}

// TestSpecValidateRejectsNonExecutableFile verifies executable permission check.
func TestSpecValidateRejectsNonExecutableFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a regular file without execute permission
	nonExecPath := filepath.Join(tmpDir, "nonexec")
	if err := os.WriteFile(nonExecPath, []byte("#!/bin/sh"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	spec := Spec{
		Label:      "com.test.app",
		Executable: nonExecPath,
		Args:       []string{},
		LogPath:    filepath.Join(tmpDir, "app.log"),
	}

	err := spec.Validate()
	if err == nil {
		t.Fatal("Validate: expected error for non-executable file, got nil")
	}

	var notRegularErr ErrNotRegularFile
	if !errors.As(err, &notRegularErr) {
		t.Fatalf("Validate: expected ErrNotRegularFile, got %T: %v", err, err)
	}
	if !strings.Contains(notRegularErr.Reason, "execute") {
		t.Errorf("Validate: reason should mention execute permission, got %q", notRegularErr.Reason)
	}
}
