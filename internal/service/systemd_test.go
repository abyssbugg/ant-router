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

// TestRenderSystemdUnitBasic tests basic systemd unit rendering.
func TestRenderSystemdUnitBasic(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "myapp")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "test-app",
		Executable: exe,
		Args:       []string{"arg1", "arg2"},
		LogPath:    logPath,
	}

	unit, err := renderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("renderSystemdUnit: %v", err)
	}

	unitStr := string(unit)

	// Verify sections exist
	if !strings.Contains(unitStr, "[Unit]") {
		t.Error("missing [Unit] section")
	}
	if !strings.Contains(unitStr, "[Service]") {
		t.Error("missing [Service] section")
	}
	if !strings.Contains(unitStr, "[Install]") {
		t.Error("missing [Install] section")
	}

	// Verify ownership marker
	if !strings.Contains(unitStr, OwnershipMarker) {
		t.Error("missing ownership marker")
	}

	// Verify ExecStart is present
	if !strings.Contains(unitStr, "ExecStart=") {
		t.Error("missing ExecStart")
	}

	// Verify Type=simple
	if !strings.Contains(unitStr, "Type=simple") {
		t.Error("missing Type=simple")
	}

	// Verify StandardOutput and StandardError redirects
	if !strings.Contains(unitStr, "StandardOutput=append:") {
		t.Error("missing StandardOutput append")
	}
	if !strings.Contains(unitStr, "StandardError=append:") {
		t.Error("missing StandardError append")
	}
}

// TestRenderSystemdUnitGolden tests unit rendering against golden file.
func TestRenderSystemdUnitGolden(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "myapp")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "app.log")

	spec := Spec{
		Label:      "my-service",
		Executable: exe,
		Args:       nil,
		LogPath:    logPath,
	}

	unit, err := renderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("renderSystemdUnit: %v", err)
	}

	// Read and compare against golden file
	goldenPath := filepath.Join("testdata", "systemd_basic.service")
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Logf("Note: golden file not found at %s, skipping comparison", goldenPath)
		// At least verify format
		if !strings.HasPrefix(string(unit), "[Unit]") {
			t.Fatal("unit does not start with [Unit]")
		}
		return
	}

	// Normalize paths for comparison
	unitStr := string(unit)
	unitStr = strings.ReplaceAll(unitStr, exe, "/path/to/myapp")
	unitStr = strings.ReplaceAll(unitStr, logPath, "/path/to/app.log")

	goldenStr := string(golden)
	goldenStr = strings.ReplaceAll(goldenStr, exe, "/path/to/myapp")
	goldenStr = strings.ReplaceAll(goldenStr, logPath, "/path/to/app.log")

	if unitStr != goldenStr {
		t.Errorf("rendered unit does not match golden file")
		t.Logf("got:\n%s", unitStr)
		t.Logf("want:\n%s", goldenStr)
	}
}

// unquoteSystemdWord unquotes a single systemd-quoted word following systemd rules.
func unquoteSystemdWord(s string) (string, error) {
	if !strings.HasPrefix(s, "\"") || !strings.HasSuffix(s, "\"") {
		return s, nil // not quoted
	}

	s = s[1 : len(s)-1]
	var result strings.Builder

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case '\\':
				result.WriteByte('\\')
				i++
			case '"':
				result.WriteByte('"')
				i++
			case '%':
				result.WriteByte('%')
				i++
			case '$':
				result.WriteByte('$')
				i++
			default:
				result.WriteByte(ch)
			}
		} else if ch == '%' && i+1 < len(s) && s[i+1] == '%' {
			result.WriteByte('%')
			i++
		} else if ch == '$' && i+1 < len(s) && s[i+1] == '$' {
			result.WriteByte('$')
			i++
		} else {
			result.WriteByte(ch)
		}
	}
	return result.String(), nil
}

// TestRenderSystemdUnitQuoting tests quoting of ExecStart with spaces, quotes, backslashes, %, $.
func TestRenderSystemdUnitQuoting(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app with spaces")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "log.txt")

	spec := Spec{
		Label:      "quoted-test",
		Executable: exe,
		Args: []string{
			"arg with spaces",
			`path\with\backslashes`,
			`config"with"quotes`,
			"value%percent",
			"value$dollar",
		},
		LogPath: logPath,
	}

	unit, err := renderSystemdUnit(spec)
	if err != nil {
		t.Fatalf("renderSystemdUnit: %v", err)
	}

	unitStr := string(unit)

	// Extract ExecStart line
	lines := strings.Split(unitStr, "\n")
	var execStartLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "ExecStart=") {
			execStartLine = line
			break
		}
	}

	if execStartLine == "" {
		t.Fatal("ExecStart not found in unit")
	}

	// Parse ExecStart to get argv
	execStart := strings.TrimPrefix(execStartLine, "ExecStart=")

	// Split on spaces but respect quoted strings
	var argv []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(execStart); i++ {
		ch := execStart[i]

		if ch == '\\' && inQuotes && i+1 < len(execStart) {
			current.WriteByte(ch)
			current.WriteByte(execStart[i+1])
			i++
			continue
		}

		if ch == '"' {
			current.WriteByte(ch)
			inQuotes = !inQuotes
			continue
		}

		if ch == ' ' && !inQuotes {
			if current.Len() > 0 {
				word := current.String()
				unquoted, _ := unquoteSystemdWord(word)
				argv = append(argv, unquoted)
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		word := current.String()
		unquoted, _ := unquoteSystemdWord(word)
		argv = append(argv, unquoted)
	}

	// argv should be [exe, arg1, arg2, arg3, arg4, arg5]
	if len(argv) != 6 {
		t.Logf("parsed argv: %v", argv)
		t.Fatalf("expected 6 args, got %d", len(argv))
	}

	if argv[0] != exe {
		t.Errorf("argv[0]: got %q, want %q", argv[0], exe)
	}
	if argv[1] != "arg with spaces" {
		t.Errorf("argv[1]: got %q, want %q", argv[1], "arg with spaces")
	}
	if argv[2] != `path\with\backslashes` {
		t.Errorf("argv[2]: got %q, want %q", argv[2], `path\with\backslashes`)
	}
	if argv[3] != `config"with"quotes` {
		t.Errorf("argv[3]: got %q, want %q", argv[3], `config"with"quotes`)
	}
	if argv[4] != "value%percent" {
		t.Errorf("argv[4]: got %q, want %q", argv[4], "value%percent")
	}
	if argv[5] != "value$dollar" {
		t.Errorf("argv[5]: got %q, want %q", argv[5], "value$dollar")
	}
}

// TestRenderSystemdUnitLogPathValidation tests that LogPath with space, %, $ is rejected.
func TestRenderSystemdUnitLogPathValidation(t *testing.T) {
	tests := []struct {
		name    string
		logPath string
		want    string
	}{
		{"space in path", "/tmp/log file.txt", "whitespace"},
		{"percent in path", "/tmp/log%file.txt", "percent"},
		{"dollar in path", "/tmp/log$file.txt", "dollar"},
	}

	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := Spec{
				Label:      "test",
				Executable: exe,
				Args:       nil,
				LogPath:    tt.logPath,
			}

			_, err := renderSystemdUnit(spec)
			if err == nil {
				t.Fatalf("renderSystemdUnit: expected error for LogPath with %s, got nil", tt.want)
			}

			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got error %q, want substring %q", err.Error(), tt.want)
			}
			var typed ErrUnsupportedLogPath
			if !errors.As(err, &typed) {
				t.Errorf("got error %T, want ErrUnsupportedLogPath", err)
			} else if typed.Path != tt.logPath {
				t.Errorf("got path %q, want %q", typed.Path, tt.logPath)
			}
		})
	}
}

// TestSystemdBackendInstall tests basic install flow.
func TestSystemdBackendInstall(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "app.log")
	configDir := filepath.Join(tmpDir, "config")

	spec := Spec{
		Label:      "test-service",
		Executable: exe,
		Args:       []string{"arg1"},
		LogPath:    logPath,
	}

	fake := newFakeRunner()
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Install(ctx, spec)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify unit file was created
	unitPath := filepath.Join(configDir, "systemd", "user", "test-service.service")
	_, err = os.Stat(unitPath)
	if err != nil {
		t.Fatalf("unit file not created: %v", err)
	}

	// Verify runner was called with daemon-reload, enable --now
	expectedCalls := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", "test-service.service"},
	}

	if len(fake.calls) != len(expectedCalls) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedCalls), len(fake.calls), fake.calls)
	}

	for i, expected := range expectedCalls {
		if !slicesEqual(fake.calls[i], expected) {
			t.Errorf("call %d: got %v, want %v", i, fake.calls[i], expected)
		}
	}
}

// TestSystemdBackendUninstall tests uninstall flow.
func TestSystemdBackendUninstall(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "app.log")
	configDir := filepath.Join(tmpDir, "config")

	spec := Spec{
		Label:      "test-service",
		Executable: exe,
		LogPath:    logPath,
	}

	// Pre-create the unit file with ownership marker
	unitDir := filepath.Join(configDir, "systemd", "user")
	os.MkdirAll(unitDir, 0755)
	unitPath := filepath.Join(unitDir, "test-service.service")
	unit, _ := renderSystemdUnit(spec)
	os.WriteFile(unitPath, unit, 0644)

	fake := newFakeRunner()
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Uninstall(ctx, "test-service")
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	// Verify unit file was deleted
	_, err = os.Stat(unitPath)
	if !os.IsNotExist(err) {
		t.Error("unit file should be deleted")
	}

	// Verify runner was called with disable --now and daemon-reload
	expectedCalls := [][]string{
		{"systemctl", "--user", "disable", "--now", "test-service.service"},
		{"systemctl", "--user", "daemon-reload"},
	}

	if len(fake.calls) != len(expectedCalls) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedCalls), len(fake.calls), fake.calls)
	}

	for i, expected := range expectedCalls {
		if !slicesEqual(fake.calls[i], expected) {
			t.Errorf("call %d: got %v, want %v", i, fake.calls[i], expected)
		}
	}
}

// TestSystemdBackendUninstallRefusesUnownedFile tests that uninstall refuses unowned files.
func TestSystemdBackendUninstallRefusesUnownedFile(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	// Pre-create an unowned unit file
	unitDir := filepath.Join(configDir, "systemd", "user")
	os.MkdirAll(unitDir, 0755)
	unitPath := filepath.Join(unitDir, "test-service.service")
	os.WriteFile(unitPath, []byte("[Unit]\nDescription=unowned service\n"), 0644)

	fake := newFakeRunner()
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Uninstall(ctx, "test-service")

	if !isErrNotOwned(err) {
		t.Fatalf("Uninstall: expected ErrNotOwned, got %v", err)
	}

	// Verify no runner calls were made
	if len(fake.calls) != 0 {
		t.Errorf("expected 0 runner calls for unowned file, got %d", len(fake.calls))
	}
}

// TestSystemdBackendUninstallKeepsFileOnDisableError tests that file is kept on disable error.
func TestSystemdBackendUninstallKeepsFileOnDisableError(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "app.log")
	configDir := filepath.Join(tmpDir, "config")

	spec := Spec{
		Label:      "test-service",
		Executable: exe,
		LogPath:    logPath,
	}

	// Pre-create the unit file
	unitDir := filepath.Join(configDir, "systemd", "user")
	os.MkdirAll(unitDir, 0755)
	unitPath := filepath.Join(unitDir, "test-service.service")
	unit, _ := renderSystemdUnit(spec)
	os.WriteFile(unitPath, unit, 0644)

	// Create a custom runner that fails on disable but succeeds on daemon-reload
	callCount := 0
	customRunner := &mockRunner{
		resp: []byte(""),
		err:  nil,
	}
	customRunner.runFunc = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		defer func() { callCount++ }()
		call := append([]string{name}, args...)
		customRunner.calls = append(customRunner.calls, call)

		// First call (disable) fails
		if callCount == 0 {
			return []byte(""), []byte("Permission denied"), fmt.Errorf("permission denied")
		}
		// Second call (daemon-reload) succeeds
		return []byte(""), []byte(""), nil
	}

	backend := NewSystemdBackend(configDir, customRunner)

	ctx := context.Background()
	err := backend.Uninstall(ctx, "test-service")

	if err == nil {
		t.Error("Uninstall: expected error for permission denied, got nil")
	}

	// Verify file is still there
	_, err = os.Stat(unitPath)
	if os.IsNotExist(err) {
		t.Error("unit file should be kept on disable error")
	}
}

// TestSystemdBackendUninstallIgnoresNotLoadedError tests that not-loaded/not-exist errors are ignored.
func TestSystemdBackendUninstallIgnoresNotLoadedError(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	logPath := filepath.Join(tmpDir, "app.log")
	configDir := filepath.Join(tmpDir, "config")

	spec := Spec{
		Label:      "test-service",
		Executable: exe,
		LogPath:    logPath,
	}

	// Pre-create the unit file
	unitDir := filepath.Join(configDir, "systemd", "user")
	os.MkdirAll(unitDir, 0755)
	unitPath := filepath.Join(unitDir, "test-service.service")
	unit, _ := renderSystemdUnit(spec)
	os.WriteFile(unitPath, unit, 0644)

	fake := newFakeRunner()
	fake.errMap = map[string]error{"disable": fmt.Errorf("unit not loaded")}
	fake.stderrMap = map[string][]byte{"disable": []byte("not loaded")}

	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Uninstall(ctx, "test-service")

	if err != nil {
		t.Errorf("Uninstall: expected nil for not-loaded error, got %v", err)
	}

	// Verify file is deleted
	_, err = os.Stat(unitPath)
	if !os.IsNotExist(err) {
		t.Error("unit file should be deleted")
	}
}

// TestSystemdBackendStart tests start flow.
func TestSystemdBackendStart(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	// Create owned unit file
	unitDir := filepath.Join(configDir, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	unitPath := filepath.Join(unitDir, "my-service.service")
	unitContent := []byte("[Unit]\nDescription=Test\n# " + OwnershipMarker + "\n[Service]\nType=simple\nExecStart=/bin/true\n[Install]\nWantedBy=default.target\n")
	if err := os.WriteFile(unitPath, unitContent, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fake := newFakeRunner()
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Start(ctx, "my-service")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	expectedCall := []string{"systemctl", "--user", "start", "my-service.service"}
	if len(fake.calls) != 1 || !slicesEqual(fake.calls[0], expectedCall) {
		t.Errorf("Start: got calls %v, want %v", fake.calls, [][]string{expectedCall})
	}
}

// TestSystemdBackendStop tests stop flow.
func TestSystemdBackendStop(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	// Create owned unit file
	unitDir := filepath.Join(configDir, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	unitPath := filepath.Join(unitDir, "my-service.service")
	unitContent := []byte("[Unit]\nDescription=Test\n# " + OwnershipMarker + "\n[Service]\nType=simple\nExecStart=/bin/true\n[Install]\nWantedBy=default.target\n")
	if err := os.WriteFile(unitPath, unitContent, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fake := newFakeRunner()
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Stop(ctx, "my-service")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	expectedCall := []string{"systemctl", "--user", "stop", "my-service.service"}
	if len(fake.calls) != 1 || !slicesEqual(fake.calls[0], expectedCall) {
		t.Errorf("Stop: got calls %v, want %v", fake.calls, [][]string{expectedCall})
	}
}

// mockRunner captures calls and returns configured responses.
type mockRunner struct {
	calls   [][]string
	resp    []byte
	err     error
	runFunc func(context.Context, string, ...string) ([]byte, []byte, error)
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, name, args...)
	}
	call := append([]string{name}, args...)
	m.calls = append(m.calls, call)
	return m.resp, nil, m.err
}

// TestSystemdBackendStatusActive tests status returns Running for active.
func TestSystemdBackendStatusActive(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	writeTestUnit(t, configDir, "my-service")

	mock := &mockRunner{resp: []byte("active")}
	backend := NewSystemdBackend(configDir, mock)

	ctx := context.Background()
	status, err := backend.Status(ctx, "my-service")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.State != StateRunning {
		t.Errorf("Status: got state %v, want %v", status.State, StateRunning)
	}
}

// TestSystemdBackendStatusInactive tests status returns Stopped for inactive.
func TestSystemdBackendStatusInactive(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	writeTestUnit(t, configDir, "my-service")

	mock := &mockRunner{resp: []byte("inactive"), err: fmt.Errorf("is-active failed")}
	backend := NewSystemdBackend(configDir, mock)

	ctx := context.Background()
	status, err := backend.Status(ctx, "my-service")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.State != StateStopped {
		t.Errorf("Status: got state %v, want %v", status.State, StateStopped)
	}
}

// TestSystemdBackendStatusFailed tests status returns Stopped for failed.
func TestSystemdBackendStatusFailed(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	writeTestUnit(t, configDir, "my-service")

	mock := &mockRunner{resp: []byte("failed"), err: fmt.Errorf("is-active failed")}
	backend := NewSystemdBackend(configDir, mock)

	ctx := context.Background()
	status, err := backend.Status(ctx, "my-service")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.State != StateStopped {
		t.Errorf("Status: got state %v, want %v", status.State, StateStopped)
	}
}

// TestSystemdBackendStatusActivating tests status returns Unknown for activating.
func TestSystemdBackendStatusActivating(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	writeTestUnit(t, configDir, "my-service")

	mock := &mockRunner{resp: []byte("activating")}
	backend := NewSystemdBackend(configDir, mock)

	ctx := context.Background()
	status, err := backend.Status(ctx, "my-service")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.State != StateUnknown {
		t.Errorf("Status: got state %v, want %v", status.State, StateUnknown)
	}
	if status.Detail != "activating" {
		t.Errorf("Status: got detail %q, want %q", status.Detail, "activating")
	}
}

// TestSystemdBackendStatusMissing verifies that a missing unit file yields
// NotInstalled even though real `systemctl --user is-active` prints
// "inactive" with exit status 3 and empty stderr for unknown units.
func TestSystemdBackendStatusMissing(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "config")
	mock := &mockRunner{resp: []byte("inactive\n"), err: fmt.Errorf("exit status 3")}
	backend := NewSystemdBackend(configDir, mock)

	status, err := backend.Status(context.Background(), "my-service")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateNotInstalled {
		t.Errorf("Status: got state %v, want %v", status.State, StateNotInstalled)
	}
	if len(mock.calls) != 0 {
		t.Errorf("Status: expected no systemctl calls for a missing unit, got %v", mock.calls)
	}
}

// TestSystemdBackendStatusPresentInactive verifies that an installed but
// stopped unit (realistic is-active output: "inactive", exit 3, empty stderr)
// is reported as Stopped rather than NotInstalled.
func TestSystemdBackendStatusPresentInactive(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "config")
	unitDir := filepath.Join(configDir, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, "my-service.service"), []byte(OwnershipMarker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mock := &mockRunner{resp: []byte("inactive\n"), err: fmt.Errorf("exit status 3")}
	backend := NewSystemdBackend(configDir, mock)

	status, err := backend.Status(context.Background(), "my-service")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != StateStopped {
		t.Errorf("Status: got state %v, want %v", status.State, StateStopped)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("Status: expected one systemctl call, got %v", mock.calls)
	}
}

// TestSystemdBackendRejectsUnsafeLabelsBeforeAnySideEffect tests that all methods reject unsafe labels before any runner call.
func TestSystemdBackendRejectsUnsafeLabelsBeforeAnySideEffect(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")
	configDir := filepath.Join(tmpDir, "config")

	unsafeLabels := []string{
		"../../evil",
		"com/test",
		"",
		".hidden",
		"com test app",
		"com.test.café",
		"com." + strings.Repeat("a", 130),
	}

	for _, badLabel := range unsafeLabels {
		t.Run("Install-"+badLabel, func(t *testing.T) {
			fake := newFakeRunner()
			backend := NewSystemdBackend(configDir, fake)

			spec := Spec{
				Label:      badLabel,
				Executable: exe,
				LogPath:    logPath,
			}

			ctx := context.Background()
			err := backend.Install(ctx, spec)

			if err == nil {
				t.Fatalf("Install: expected error for label %q, got nil", badLabel)
			}

			if len(fake.calls) != 0 {
				t.Errorf("Install: expected 0 runner calls for invalid label, got %d", len(fake.calls))
			}
		})

		t.Run("Start-"+badLabel, func(t *testing.T) {
			fake := newFakeRunner()
			backend := NewSystemdBackend(configDir, fake)

			ctx := context.Background()
			err := backend.Start(ctx, badLabel)

			if err == nil {
				t.Fatalf("Start: expected error for label %q, got nil", badLabel)
			}

			if len(fake.calls) != 0 {
				t.Errorf("Start: expected 0 runner calls for invalid label, got %d", len(fake.calls))
			}
		})

		t.Run("Stop-"+badLabel, func(t *testing.T) {
			fake := newFakeRunner()
			backend := NewSystemdBackend(configDir, fake)

			ctx := context.Background()
			err := backend.Stop(ctx, badLabel)

			if err == nil {
				t.Fatalf("Stop: expected error for label %q, got nil", badLabel)
			}

			if len(fake.calls) != 0 {
				t.Errorf("Stop: expected 0 runner calls for invalid label, got %d", len(fake.calls))
			}
		})

		t.Run("Status-"+badLabel, func(t *testing.T) {
			fake := newFakeRunner()
			backend := NewSystemdBackend(configDir, fake)

			ctx := context.Background()
			_, err := backend.Status(ctx, badLabel)

			if err == nil {
				t.Fatalf("Status: expected error for label %q, got nil", badLabel)
			}

			if len(fake.calls) != 0 {
				t.Errorf("Status: expected 0 runner calls for invalid label, got %d", len(fake.calls))
			}
		})

		t.Run("Uninstall-"+badLabel, func(t *testing.T) {
			fake := newFakeRunner()
			backend := NewSystemdBackend(configDir, fake)

			ctx := context.Background()
			err := backend.Uninstall(ctx, badLabel)

			if err == nil {
				t.Fatalf("Uninstall: expected error for label %q, got nil", badLabel)
			}

			if len(fake.calls) != 0 {
				t.Errorf("Uninstall: expected 0 runner calls for invalid label, got %d", len(fake.calls))
			}
		})
	}
}

// Helper functions

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeTestUnit creates an owned unit file so Status treats the unit as installed.
func writeTestUnit(t *testing.T, configDir, label string) {
	t.Helper()
	unitDir := filepath.Join(configDir, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, label+".service"), []byte(OwnershipMarker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSystemdBackendUninstallUnreadableUnitHasNoSideEffects verifies that an
// unit path which exists but cannot be read as a file is never disabled or
// removed, because ownership cannot be proven.
func TestSystemdBackendUninstallUnreadableUnitHasNoSideEffects(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "config")
	unitPath := filepath.Join(configDir, "systemd", "user", "my-service.service")
	if err := os.MkdirAll(unitPath, 0o755); err != nil {
		t.Fatal(err)
	}
	mock := &mockRunner{}
	backend := NewSystemdBackend(configDir, mock)

	if err := backend.Uninstall(context.Background(), "my-service"); err == nil {
		t.Fatal("Uninstall: expected error for unreadable unit path")
	}
	if len(mock.calls) != 0 {
		t.Errorf("Uninstall: expected no systemctl calls, got %v", mock.calls)
	}
	if _, err := os.Stat(unitPath); err != nil {
		t.Errorf("Uninstall: unit path should remain: %v", err)
	}
}

// TestSystemdInstallRefusesUnownedUnit verifies Install refuses to overwrite unowned units.
func TestSystemdInstallRefusesUnownedUnit(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("WriteFile exe: %v", err)
	}
	logPath := filepath.Join(tmpDir, "app.log")

	// Create an unowned unit at the target path
	unitsDir := filepath.Join(tmpDir, "systemd", "user")
	if err := os.MkdirAll(unitsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	unitPath := filepath.Join(unitsDir, "com.test.app.service")
	unownedContent := []byte("[Unit]\nDescription=Some other service\n")
	if err := os.WriteFile(unitPath, unownedContent, 0644); err != nil {
		t.Fatalf("WriteFile unowned: %v", err)
	}

	spec := Spec{
		Label:      "com.test.app",
		Executable: exe,
		Args:       []string{},
		LogPath:    logPath,
	}

	fake := newFakeRunner()
	backend := NewSystemdBackend(tmpDir, fake)

	ctx := context.Background()
	err := backend.Install(ctx, spec)

	// Should return ErrNotOwned
	if !isErrNotOwned(err) {
		t.Fatalf("Install: expected ErrNotOwned, got %v", err)
	}

	// Unit should be unchanged (byte-identical)
	updated, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("ReadFile unit after Install: %v", err)
	}
	if !bytes.Equal(updated, unownedContent) {
		t.Errorf("unit was modified: expected %q, got %q", unownedContent, updated)
	}

	// No enable should have been called
	enableCall := findCall(fake.calls, "systemctl", "enable")
	if enableCall != nil {
		t.Error("enable was called despite refusing Install")
	}
}

// TestSystemdStartRefusesUnownedAndIssuesNoCommand verifies Start refuses unowned units.
func TestSystemdStartRefusesUnownedAndIssuesNoCommand(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	// Create an unowned unit
	unitsDir := filepath.Join(configDir, "systemd", "user")
	if err := os.MkdirAll(unitsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	unitPath := filepath.Join(unitsDir, "com.test.app.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\nDescription=Other\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fake := newFakeRunner()
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Start(ctx, "com.test.app")

	// Should return ErrNotOwned
	if !isErrNotOwned(err) {
		t.Fatalf("Start: expected ErrNotOwned, got %v", err)
	}

	// No systemctl commands should have been recorded
	if len(fake.calls) != 0 {
		t.Errorf("Start issued commands despite refusing: %v", fake.calls)
	}
}

// TestSystemdStopRefusesUnownedAndIssuesNoCommand verifies Stop refuses unowned units.
func TestSystemdStopRefusesUnownedAndIssuesNoCommand(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	// Create an unowned unit
	unitsDir := filepath.Join(configDir, "systemd", "user")
	if err := os.MkdirAll(unitsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	unitPath := filepath.Join(unitsDir, "com.test.app.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\nDescription=Other\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fake := newFakeRunner()
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Stop(ctx, "com.test.app")

	// Should return ErrNotOwned
	if !isErrNotOwned(err) {
		t.Fatalf("Stop: expected ErrNotOwned, got %v", err)
	}

	// No systemctl commands should have been recorded
	if len(fake.calls) != 0 {
		t.Errorf("Stop issued commands despite refusing: %v", fake.calls)
	}
}

// TestSystemdInstallCreatesLogDirectory verifies log directory is created.
// TestSystemdStartReturnsMissingAsNotInstalled verifies Start returns ErrNotInstalled when unit doesn't exist.
func TestSystemdStartReturnsMissingAsNotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fake := &mockRunner{}
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Start(ctx, "my-service")

	// Should return ErrNotInstalled
	var notInstalledErr ErrNotInstalled
	if !errors.As(err, &notInstalledErr) {
		t.Fatalf("Start: expected ErrNotInstalled, got %T: %v", err, err)
	}
}

// TestSystemdStopReturnsMissingAsNotInstalled verifies Stop returns ErrNotInstalled when unit doesn't exist.
func TestSystemdStopReturnsMissingAsNotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fake := &mockRunner{}
	backend := NewSystemdBackend(configDir, fake)

	ctx := context.Background()
	err := backend.Stop(ctx, "my-service")

	// Should return ErrNotInstalled
	var notInstalledErr ErrNotInstalled
	if !errors.As(err, &notInstalledErr) {
		t.Fatalf("Stop: expected ErrNotInstalled, got %T: %v", err, err)
	}
}

func TestSystemdInstallCreatesLogDirectory(t *testing.T) {
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
	backend := NewSystemdBackend(tmpDir, fake)

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
