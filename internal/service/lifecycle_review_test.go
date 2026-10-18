package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeOwnedPlist(t *testing.T, home, label string) string {
	t.Helper()
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, label+".plist")
	if err := os.WriteFile(path, []byte("<!-- "+OwnershipMarker+" -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testSpec(t *testing.T, dir string) Spec {
	t.Helper()
	exe := filepath.Join(dir, "routerd")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Spec{Label: "com.test.app", Executable: exe, LogPath: filepath.Join(dir, "logs", "routerd.log")}
}

// With KeepAlive{SuccessfulExit=false}, SIGTERM is followed by a launchd
// restart; Stop must unload the job instead.
func TestLaunchdStopUnloadsJobSoKeepAliveCannotRestartIt(t *testing.T) {
	home := t.TempDir()
	writeOwnedPlist(t, home, "com.test.app")
	fake := newFakeRunner()
	if err := NewLaunchdBackend(home, 501, fake).Stop(context.Background(), "com.test.app"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"launchctl", "bootout", "gui/501/com.test.app"}}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

func TestLaunchdStopIsIdempotentWhenNotLoaded(t *testing.T) {
	home := t.TempDir()
	writeOwnedPlist(t, home, "com.test.app")
	fake := newFakeRunner()
	fake.errMap["bootout"] = errors.New("exit status 113")
	if err := NewLaunchdBackend(home, 501, fake).Stop(context.Background(), "com.test.app"); err != nil {
		t.Fatalf("Stop of an unloaded job = %v, want nil", err)
	}
	fake = newFakeRunner()
	fake.errMap["bootout"] = errors.New("exit status 5")
	fake.stderrMap["bootout"] = []byte("Boot-out failed: 5: Input/output error")
	if err := NewLaunchdBackend(home, 501, fake).Stop(context.Background(), "com.test.app"); err == nil {
		t.Fatal("Stop hid a real bootout failure")
	}
}

func TestLaunchdStartBootstrapsUnloadedJob(t *testing.T) {
	home := t.TempDir()
	plistPath := writeOwnedPlist(t, home, "com.test.app")
	fake := newFakeRunner()
	fake.errMap["print"] = errors.New("exit status 113")
	if err := NewLaunchdBackend(home, 501, fake).Start(context.Background(), "com.test.app"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"launchctl", "print", "gui/501/com.test.app"},
		{"launchctl", "bootstrap", "gui/501", plistPath},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

// kickstart -k kills a running job; Start must leave a running job alone.
func TestLaunchdStartDoesNotInterruptLoadedJob(t *testing.T) {
	home := t.TempDir()
	writeOwnedPlist(t, home, "com.test.app")
	fake := newFakeRunner()
	if err := NewLaunchdBackend(home, 501, fake).Start(context.Background(), "com.test.app"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"launchctl", "print", "gui/501/com.test.app"},
		{"launchctl", "kickstart", "gui/501/com.test.app"},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

func TestLaunchdStartSurfacesUnexpectedPrintFailure(t *testing.T) {
	home := t.TempDir()
	writeOwnedPlist(t, home, "com.test.app")
	fake := newFakeRunner()
	fake.errMap["print"] = errors.New("exit status 1")
	fake.stderrMap["print"] = []byte("permission denied")
	if err := NewLaunchdBackend(home, 501, fake).Start(context.Background(), "com.test.app"); err == nil {
		t.Fatal("Start ignored an unexpected print failure")
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls after failed print = %v", fake.calls)
	}
}

// After Stop (bootout) the job is unloaded but still installed.
func TestLaunchdStatusReportsInstalledUnloadedJobAsStopped(t *testing.T) {
	home := t.TempDir()
	fake := newFakeRunner()
	fake.errMap["print"] = errors.New("exit status 113")
	backend := NewLaunchdBackend(home, 501, fake)
	status, err := backend.Status(context.Background(), "com.test.app")
	if err != nil || status.State != StateNotInstalled {
		t.Fatalf("missing plist: status=%v err=%v, want not installed", status, err)
	}
	writeOwnedPlist(t, home, "com.test.app")
	status, err = backend.Status(context.Background(), "com.test.app")
	if err != nil || status.State != StateStopped {
		t.Fatalf("installed unloaded job: status=%v err=%v, want stopped", status, err)
	}
}

func TestInstallChecksOwnershipBeforeAnySideEffect(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)

	home := filepath.Join(dir, "home")
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plistDir, spec.Label+".plist"), []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeRunner()
	err := NewLaunchdBackend(home, 501, fake).Install(context.Background(), spec)
	var notOwned ErrNotOwned
	if !errors.As(err, &notOwned) {
		t.Fatalf("launchd Install err = %v, want ErrNotOwned", err)
	}

	config := filepath.Join(dir, "config")
	unitDir := filepath.Join(config, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, spec.Label+".service"), []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	sysFake := newFakeRunner()
	err = NewSystemdBackend(config, sysFake).Install(context.Background(), spec)
	if !errors.As(err, &notOwned) {
		t.Fatalf("systemd Install err = %v, want ErrNotOwned", err)
	}
	if _, statErr := os.Stat(filepath.Dir(spec.LogPath)); !os.IsNotExist(statErr) {
		t.Fatalf("log directory created before ownership check (stat err %v)", statErr)
	}
	if len(fake.calls) != 0 || len(sysFake.calls) != 0 {
		t.Fatalf("commands ran for unowned definitions: %v %v", fake.calls, sysFake.calls)
	}
}

func TestSystemdRejectsBackslashInLogPath(t *testing.T) {
	spec := testSpec(t, t.TempDir())
	for _, logPath := range []string{`/tmp/logs\`, `/tmp/a\nb`} {
		spec.LogPath = logPath
		_, err := renderSystemdUnit(spec)
		var unsupported ErrUnsupportedLogPath
		if !errors.As(err, &unsupported) || !strings.Contains(unsupported.Reason, "backslash") {
			t.Fatalf("renderSystemdUnit(%q) err = %v, want backslash ErrUnsupportedLogPath", logPath, err)
		}
	}
}

func TestSystemdInstallRequiresAppendCapableSystemd(t *testing.T) {
	cases := []struct {
		version string
		ok      bool
		typed   bool
	}{
		{"systemd 239 (239-78.el8)\n+PAM", false, true},
		{"systemd 240\n", true, false},
		{"systemd 255 (255.4-1ubuntu8)\n", true, false},
		{"not systemd\n", false, false},
		{"fakesd 300\n", false, false},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		spec := testSpec(t, dir)
		config := filepath.Join(dir, "config")
		fake := newFakeRunner()
		fake.stdoutMap["--version"] = []byte(tc.version)
		err := NewSystemdBackend(config, fake).Install(context.Background(), spec)
		unitPath := filepath.Join(config, "systemd", "user", spec.Label+".service")
		_, statErr := os.Stat(unitPath)
		if tc.ok {
			if err != nil || statErr != nil {
				t.Fatalf("%q: Install err=%v unit stat=%v, want success", tc.version, err, statErr)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%q: Install succeeded, want rejection", tc.version)
		}
		var unsupported ErrUnsupportedSystemd
		if tc.typed && (!errors.As(err, &unsupported) || unsupported.Version != 239) {
			t.Fatalf("%q: err = %v, want ErrUnsupportedSystemd{239}", tc.version, err)
		}
		if !os.IsNotExist(statErr) {
			t.Fatalf("%q: unit written despite rejection", tc.version)
		}
		if want := [][]string{{"systemctl", "--version"}}; !reflect.DeepEqual(fake.calls, want) {
			t.Fatalf("%q: calls = %v, want only the version probe", tc.version, fake.calls)
		}
	}
}
