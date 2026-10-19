package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeOwnedPlist(t *testing.T, home, label string) string {
	t.Helper()
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, label+".plist")
	if err := os.WriteFile(path, []byte(launchdOwnerLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustMkdirAll(t *testing.T, dir string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(dir, perm); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
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

// A foreign definition that merely mentions the marker as data (a program
// argument, a description) is not owned: ownership is a dedicated line.
func TestOwnershipRequiresStructuralMarker(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	spec.Args = []string{"--note", OwnershipMarker}

	t.Run("launchd", func(t *testing.T) {
		home := t.TempDir()
		plist, err := renderLaunchdPlist(spec)
		if err != nil {
			t.Fatal(err)
		}
		foreign := strings.Replace(string(plist), launchdOwnerLine+"\n", "", 1)
		if foreign == string(plist) {
			t.Fatal("owner line not found in rendered plist")
		}
		agents := filepath.Join(home, "Library", "LaunchAgents")
		if err := os.MkdirAll(agents, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agents, spec.Label+".plist"), []byte(foreign), 0o644); err != nil {
			t.Fatal(err)
		}
		fake := newFakeRunner()
		err = NewLaunchdBackend(home, 501, fake).Uninstall(context.Background(), spec.Label)
		if !isErrNotOwned(err) {
			t.Fatalf("Uninstall = %v, want ErrNotOwned", err)
		}
		if len(fake.calls) != 0 {
			t.Fatalf("no launchctl calls expected, got %v", fake.calls)
		}
	})

	t.Run("systemd", func(t *testing.T) {
		config := t.TempDir()
		unit, err := renderSystemdUnit(spec)
		if err != nil {
			t.Fatal(err)
		}
		foreign := strings.Replace(string(unit), systemdOwnerLine+"\n", "", 1)
		if foreign == string(unit) {
			t.Fatal("owner line not found in rendered unit")
		}
		unitDir := filepath.Join(config, "systemd", "user")
		if err := os.MkdirAll(unitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(unitDir, spec.Label+".service"), []byte(foreign), 0o644); err != nil {
			t.Fatal(err)
		}
		fake := newFakeRunner()
		err = NewSystemdBackend(config, fake).Uninstall(context.Background(), spec.Label)
		if !isErrNotOwned(err) {
			t.Fatalf("Uninstall = %v, want ErrNotOwned", err)
		}
		if len(fake.calls) != 0 {
			t.Fatalf("no systemctl calls expected, got %v", fake.calls)
		}
	})

	t.Run("systemd owner line inside a continued value", func(t *testing.T) {
		data := []byte("[Unit]\nDescription=x \\\n" + systemdOwnerLine + "\n")
		if hasOwnerLine(data, systemdOwnerLine) {
			t.Fatal("an owner line continuing a previous value must not count")
		}
	})
}

// Without our definition the label may belong to a unit or agent loaded from
// another directory, so Uninstall must not issue label-based commands.
func TestUninstallMissingDefinitionNeverTouchesLabel(t *testing.T) {
	t.Run("launchd", func(t *testing.T) {
		fake := newFakeRunner()
		if err := NewLaunchdBackend(t.TempDir(), 501, fake).Uninstall(context.Background(), "com.example.app"); err != nil {
			t.Fatalf("Uninstall = %v, want nil", err)
		}
		if len(fake.calls) != 0 {
			t.Fatalf("no launchctl calls expected, got %v", fake.calls)
		}
	})
	t.Run("systemd only reloads", func(t *testing.T) {
		fake := newFakeRunner()
		if err := NewSystemdBackend(t.TempDir(), fake).Uninstall(context.Background(), "example"); err != nil {
			t.Fatalf("Uninstall = %v, want nil", err)
		}
		// daemon-reload is label-free and completes an earlier uninstall
		// whose reload failed after the unit file was removed.
		want := [][]string{{"systemctl", "--user", "daemon-reload"}}
		if !reflect.DeepEqual(fake.calls, want) {
			t.Fatalf("calls = %v, want %v", fake.calls, want)
		}
	})
}

func TestSystemdInstallUndoesEnableWhenStartFails(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	fake := newFakeRunner()
	fake.errMap["enable"] = errors.New("exit status 1")
	fake.stderrMap["enable"] = []byte("Job for x.service failed")
	err := NewSystemdBackend(filepath.Join(dir, "config"), fake).Install(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "enable") {
		t.Fatalf("Install = %v, want enable error", err)
	}
	last := fake.calls[len(fake.calls)-1]
	want := []string{"systemctl", "--user", "disable", spec.Label + ".service"}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("last call = %v, want %v", last, want)
	}
}

func TestExecRunnerOnlyRunsServiceManagers(t *testing.T) {
	_, _, err := (&ExecRunner{}).Run(context.Background(), "sh", "-c", "true")
	if err == nil || !strings.Contains(err.Error(), "not permitted") {
		t.Fatalf("Run(sh) = %v, want not permitted", err)
	}
}

// lockingRunner blocks the first matching command until released so a test
// can observe whether a concurrent lifecycle call proceeds meanwhile.
type lockingRunner struct {
	mu      sync.Mutex
	calls   [][]string
	blockOn string
	entered chan struct{}
	release chan struct{}
}

func (r *lockingRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	block := len(args) > 1 && args[1] == r.blockOn
	if block {
		r.blockOn = ""
	}
	r.mu.Unlock()
	if block {
		close(r.entered)
		<-r.release
	}
	if len(args) > 0 && args[0] == "--version" {
		return []byte("systemd 252\n"), nil, nil
	}
	return nil, nil, nil
}

func (r *lockingRunner) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

func TestSystemdLifecycleSerializedPerLabel(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	runner := &lockingRunner{blockOn: "enable", entered: make(chan struct{}), release: make(chan struct{})}
	backend := NewSystemdBackend(filepath.Join(dir, "config"), runner)

	installErr := make(chan error, 1)
	go func() { installErr <- backend.Install(context.Background(), spec) }()
	<-runner.entered

	uninstallErr := make(chan error, 1)
	go func() { uninstallErr <- backend.Uninstall(context.Background(), spec.Label) }()
	time.Sleep(200 * time.Millisecond)
	for _, call := range runner.snapshot() {
		if len(call) > 2 && call[2] == "disable" {
			t.Fatalf("Uninstall ran while Install held the label: %v", runner.snapshot())
		}
	}
	close(runner.release)
	if err := <-installErr; err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := <-uninstallErr; err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	calls := runner.snapshot()
	last := calls[len(calls)-1]
	if !reflect.DeepEqual(last, []string{"systemctl", "--user", "daemon-reload"}) {
		t.Fatalf("Uninstall did not complete after Install: %v", calls)
	}
}

// The allowlist must be enforced where the *exec.Cmd is built, not only in
// ExecRunner.Run, so no in-package caller can construct an arbitrary command.
func TestServiceCommandOnlyBuildsServiceManagers(t *testing.T) {
	if _, err := serviceCommand(context.Background(), "sh", "-c", "true"); err == nil || !strings.Contains(err.Error(), "not permitted") {
		t.Fatalf("serviceCommand(sh) = %v, want not permitted", err)
	}
	for _, name := range []string{"launchctl", "systemctl"} {
		cmd, err := serviceCommand(context.Background(), name, "--version")
		if err != nil {
			t.Fatalf("serviceCommand(%s): %v", name, err)
		}
		if got := filepath.Base(cmd.Args[0]); got != name || len(cmd.Args) != 2 || cmd.Args[1] != "--version" {
			t.Fatalf("serviceCommand(%s).Args = %q", name, cmd.Args)
		}
	}
}
