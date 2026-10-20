package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// notLoaded makes launchctl print report the label as not loaded, the state
// of a label no launchd job uses.
func notLoaded(fake *fakeRunner) {
	fake.errMap["print"] = errors.New("exit status 113")
	fake.stderrMap["print"] = []byte("Could not find service \"com.test.app\" in domain for user gui: 501")
}

// Without an owned plist the label may name an agent loaded from another
// directory; Install must refuse rather than boot it out.
func TestLaunchdInstallRefusesLabelLoadedWithoutOwnedPlist(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	home := filepath.Join(dir, "home")
	fake := newFakeRunner()
	fake.stdoutMap["print"] = []byte("gui/501/com.test.app = {\n\tstate = running\n}\n")
	err := NewLaunchdBackend(home, 501, fake).Install(context.Background(), spec)
	var inUse ErrLabelInUse
	if !errors.As(err, &inUse) || inUse.Label != spec.Label {
		t.Fatalf("Install err = %v, want ErrLabelInUse for %s", err, spec.Label)
	}
	for _, call := range fake.calls {
		if len(call) > 1 && (call[1] == "bootout" || call[1] == "bootstrap") {
			t.Fatalf("Install touched a foreign job: %v", fake.calls)
		}
	}
	if _, statErr := os.Stat(filepath.Join(home, "Library", "LaunchAgents", spec.Label+".plist")); !os.IsNotExist(statErr) {
		t.Fatalf("plist written for a foreign label (stat err %v)", statErr)
	}
}

func TestLaunchdFreshInstallBootstrapsWithoutBootout(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	fake := newFakeRunner()
	notLoaded(fake)
	if err := NewLaunchdBackend(filepath.Join(dir, "home"), 501, fake).Install(context.Background(), spec); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if findCall(fake.calls, "launchctl", "bootout") != nil {
		t.Fatalf("fresh install booted out a job it does not own: %v", fake.calls)
	}
	if findCall(fake.calls, "launchctl", "bootstrap") == nil {
		t.Fatalf("bootstrap not called: %v", fake.calls)
	}
}

// Reinstalling over our own plist reloads our job: bootout then bootstrap.
func TestLaunchdReinstallBootsOutOwnedJob(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	home := filepath.Join(dir, "home")
	writeOwnedPlist(t, home, spec.Label)
	fake := newFakeRunner()
	if err := NewLaunchdBackend(home, 501, fake).Install(context.Background(), spec); err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := [][]string{
		{"launchctl", "bootout", "gui/501/" + spec.Label},
		{"launchctl", "bootstrap", "gui/501", filepath.Join(home, "Library", "LaunchAgents", spec.Label+".plist")},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

func TestLaunchdInstallSurfacesUnexpectedPrintFailure(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	fake := newFakeRunner()
	fake.errMap["print"] = errors.New("exit status 5")
	fake.stderrMap["print"] = []byte("Bad request.")
	err := NewLaunchdBackend(filepath.Join(dir, "home"), 501, fake).Install(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "Bad request") {
		t.Fatalf("Install err = %v, want query failure", err)
	}
	if findCall(fake.calls, "launchctl", "bootstrap") != nil {
		t.Fatalf("bootstrap ran after an unknown query failure: %v", fake.calls)
	}
}

// A definition that cannot be inspected is not proof the service is absent.
func TestLaunchdStatusSurfacesDefinitionStatError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions enforced for the current user")
	}
	home := t.TempDir()
	writeOwnedPlist(t, home, "com.test.app")
	agents := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.Chmod(agents, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agents, 0o755) })
	fake := newFakeRunner()
	notLoaded(fake)
	status, err := NewLaunchdBackend(home, 501, fake).Status(context.Background(), "com.test.app")
	if err == nil {
		t.Fatalf("Status = %v, nil; want the stat error", status.State)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Status err = %v, want permission error", err)
	}
}

func TestSystemdInstallEnablesThenRestarts(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	fake := newFakeRunner()
	if err := NewSystemdBackend(filepath.Join(dir, "config"), fake).Install(context.Background(), spec); err != nil {
		t.Fatalf("Install: %v", err)
	}
	unit := spec.Label + ".service"
	want := [][]string{
		{"systemctl", "--version"},
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "is-enabled", unit},
		{"systemctl", "--user", "enable", unit},
		// restart, not start: a reinstall must run the new definition.
		{"systemctl", "--user", "restart", unit},
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

// Rollback undoes only the enablement this Install created; a unit that was
// already enabled stays enabled.
func TestSystemdInstallRollbackDisablesOnlyNewEnablement(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failing     string
		wasEnabled  bool
		wantDisable bool
	}{
		{"enable fails on fresh unit", "enable", false, true},
		{"restart fails on fresh unit", "restart", false, true},
		{"enable fails on enabled unit", "enable", true, false},
		{"restart fails on enabled unit", "restart", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			spec := testSpec(t, dir)
			fake := newFakeRunner()
			if tc.wasEnabled {
				fake.stdoutMap["is-enabled"] = []byte("enabled\n")
			}
			fake.errMap[tc.failing] = errors.New("exit status 1")
			fake.stderrMap[tc.failing] = []byte("Job for x.service failed")
			err := NewSystemdBackend(filepath.Join(dir, "config"), fake).Install(context.Background(), spec)
			if err == nil || !strings.Contains(err.Error(), tc.failing) {
				t.Fatalf("Install = %v, want %s error", err, tc.failing)
			}
			disabled := false
			for _, call := range fake.calls {
				if len(call) > 2 && call[2] == "disable" {
					disabled = true
					if want := []string{"systemctl", "--user", "disable", spec.Label + ".service"}; !reflect.DeepEqual(call, want) {
						t.Fatalf("disable call = %v, want %v", call, want)
					}
				}
			}
			if disabled != tc.wantDisable {
				t.Fatalf("disable ran = %v, want %v: %v", disabled, tc.wantDisable, fake.calls)
			}
		})
	}
}

func TestSystemdInstallReportsFailedRollback(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	fake := newFakeRunner()
	restartErr := errors.New("restart exit status 1")
	disableErr := errors.New("disable exit status 1")
	fake.errMap["restart"] = restartErr
	fake.errMap["disable"] = disableErr
	err := NewSystemdBackend(filepath.Join(dir, "config"), fake).Install(context.Background(), spec)
	if !errors.Is(err, restartErr) || !errors.Is(err, disableErr) {
		t.Fatalf("Install err = %v, want both the restart and the rollback failure", err)
	}
	if !strings.Contains(err.Error(), "undo enable") {
		t.Fatalf("Install err = %v, want the rollback failure labelled", err)
	}
}

// cancellingRunner cancels the caller's context when restart runs, the way
// an interrupted CLI would, and records the context the rollback receives.
type cancellingRunner struct {
	*fakeRunner
	cancel       context.CancelFunc
	disableErr   error
	disableBound bool
}

func (r *cancellingRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	if len(args) > 1 && args[1] == "restart" {
		r.cancel()
	}
	if len(args) > 1 && args[1] == "disable" {
		r.disableErr = ctx.Err()
		_, r.disableBound = ctx.Deadline()
	}
	return r.fakeRunner.Run(ctx, name, args...)
}

func TestSystemdInstallRollbackSurvivesCancellation(t *testing.T) {
	dir := t.TempDir()
	spec := testSpec(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &cancellingRunner{fakeRunner: newFakeRunner(), cancel: cancel}
	runner.errMap["restart"] = context.Canceled
	err := NewSystemdBackend(filepath.Join(dir, "config"), runner).Install(ctx, spec)
	if err == nil {
		t.Fatal("Install succeeded despite a failed restart")
	}
	if !hasDisable(runner.calls) {
		t.Fatalf("rollback disable did not run: %v", runner.calls)
	}
	if runner.disableErr != nil {
		t.Fatalf("rollback ran with a cancelled context: %v", runner.disableErr)
	}
	if !runner.disableBound {
		t.Fatal("rollback context has no deadline; cleanup must be bounded")
	}
}

func hasDisable(calls [][]string) bool {
	for _, call := range calls {
		if len(call) > 2 && call[2] == "disable" {
			return true
		}
	}
	return false
}
