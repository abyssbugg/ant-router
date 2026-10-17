package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/service"
)

const serviceSecretSentinel = "SECRET-backend-output-7f3a"

type fakeServiceManager struct {
	installErr, uninstallErr, startErr, stopErr, statusErr error
	status                                                 service.Status
	installed                                              *service.Spec
	calls                                                  []string
	labels                                                 []string
}

func (m *fakeServiceManager) record(call, label string) {
	m.calls = append(m.calls, call)
	m.labels = append(m.labels, label)
}

func (m *fakeServiceManager) Install(_ context.Context, spec service.Spec) error {
	m.record("install", spec.Label)
	copied := spec
	m.installed = &copied
	return m.installErr
}
func (m *fakeServiceManager) Uninstall(_ context.Context, label string) error {
	m.record("uninstall", label)
	return m.uninstallErr
}
func (m *fakeServiceManager) Start(_ context.Context, label string) error {
	m.record("start", label)
	return m.startErr
}
func (m *fakeServiceManager) Stop(_ context.Context, label string) error {
	m.record("stop", label)
	return m.stopErr
}
func (m *fakeServiceManager) Status(_ context.Context, label string) (service.Status, error) {
	m.record("status", label)
	return m.status, m.statusErr
}

type serviceHarness struct {
	manager    *fakeServiceManager
	managerErr error
	stateDirs  []string
	routerd    string
	lookupErr  error
	lookups    int
}

func (h *serviceHarness) deps() serviceDeps {
	return serviceDeps{
		newManager: func(stateDir string) (service.Manager, error) {
			h.stateDirs = append(h.stateDirs, stateDir)
			if h.managerErr != nil {
				return nil, h.managerErr
			}
			return h.manager, nil
		},
		lookupRouterd: func() (string, error) {
			h.lookups++
			return h.routerd, h.lookupErr
		},
	}
}

func newServiceHarness(t *testing.T) *serviceHarness {
	t.Helper()
	routerd := filepath.Join(t.TempDir(), "routerd")
	if err := os.WriteFile(routerd, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return &serviceHarness{manager: &fakeServiceManager{status: service.Status{State: service.StateRunning}}, routerd: routerd}
}

func runServiceCLI(t *testing.T, h *serviceHarness, options cliOptions, args ...string) (int, string, string) {
	t.Helper()
	_, command, err := parseCLI(append([]string{"service"}, args...), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseCLI(%q) error = %v", args, err)
	}
	var stdout, stderr bytes.Buffer
	code := executeServiceCommand(context.Background(), options, command, &stdout, &stderr, h.deps())
	return code, stdout.String(), stderr.String()
}

func testServiceOptions(t *testing.T) cliOptions {
	t.Helper()
	return cliOptions{StateRoot: filepath.Join(t.TempDir(), "state"), RuntimeRoot: filepath.Join(t.TempDir(), "run"), Instance: "work"}
}

func TestParseServiceAndDoctorArity(t *testing.T) {
	valid := [][]string{
		{"service", "status"}, {"service", "start"}, {"service", "stop"}, {"service", "uninstall"},
		{"service", "install"}, {"service", "install", "--routerd", "/opt/routerd"}, {"doctor"},
	}
	for _, args := range valid {
		if _, _, err := parseCLI(args, &bytes.Buffer{}); err != nil {
			t.Errorf("parseCLI(%q) error = %v", args, err)
		}
	}
	invalid := [][]string{
		{"service"}, {"service", "bogus"}, {"service", "status", "extra"}, {"service", "start", "x"},
		{"service", "install", "--routerd"}, {"service", "install", "/opt/routerd"},
		{"service", "install", "--routerd", "/a", "/b"}, {"service", "uninstall", "--routerd", "/a"}, {"doctor", "extra"},
	}
	for _, args := range invalid {
		if _, _, err := parseCLI(args, &bytes.Buffer{}); !errors.Is(err, errUsage) {
			t.Errorf("parseCLI(%q) error = %v, want usage", args, err)
		}
	}
}

func TestServiceInstallBuildsDaemonSpec(t *testing.T) {
	h := newServiceHarness(t)
	options := testServiceOptions(t)
	options.JSON = true
	code, stdout, stderr := runServiceCLI(t, h, options, "install", "--routerd", h.routerd)
	if code != ExitSuccess || stderr != "" {
		t.Fatalf("code = %d stderr = %q", code, stderr)
	}
	if h.lookups != 0 {
		t.Fatalf("explicit --routerd must not trigger sibling lookup")
	}
	paths, err := resolveCLIPaths(options)
	if err != nil {
		t.Fatal(err)
	}
	spec := h.manager.installed
	if spec == nil {
		t.Fatal("Install was not called")
	}
	if spec.Label != "dev.antrouter.routerd.work" || spec.Executable != h.routerd {
		t.Fatalf("spec = %+v", spec)
	}
	wantArgs := []string{"--instance", "work", "--state-dir", paths.StateRoot, "--runtime-dir", paths.RuntimeRoot}
	if !slices.Equal(spec.Args, wantArgs) {
		t.Fatalf("args = %q, want %q", spec.Args, wantArgs)
	}
	logDir := filepath.Join(paths.InstanceDir, "logs")
	if spec.LogPath != filepath.Join(logDir, "routerd.log") {
		t.Fatalf("log path = %q", spec.LogPath)
	}
	info, err := os.Stat(logDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("log dir: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("log dir mode = %#o, want 0700", info.Mode().Perm())
	}
	if !slices.Equal(h.stateDirs, []string{filepath.Join(paths.InstanceDir, "service")}) {
		t.Fatalf("manager state dirs = %q", h.stateDirs)
	}
	if !slices.Equal(h.manager.calls, []string{"install", "status"}) {
		t.Fatalf("calls = %q", h.manager.calls)
	}
	var result serviceResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	want := serviceResult{SchemaVersion: cliSchemaVersion, Action: "install", Label: "dev.antrouter.routerd.work", State: "running"}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
}

func TestServiceInstallUsesSiblingRouterdByDefault(t *testing.T) {
	h := newServiceHarness(t)
	code, _, stderr := runServiceCLI(t, h, testServiceOptions(t), "install")
	if code != ExitSuccess || stderr != "" {
		t.Fatalf("code = %d stderr = %q", code, stderr)
	}
	if h.lookups != 1 || h.manager.installed == nil || h.manager.installed.Executable != h.routerd {
		t.Fatalf("lookups = %d installed = %+v", h.lookups, h.manager.installed)
	}
}

func TestServiceInstallRejectsBadRouterd(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		args      []string
		lookupErr error
		message   string
	}{
		"relative":    {args: []string{"install", "--routerd", "bin/routerd"}, message: "routerd path must be an absolute path to a regular file"},
		"missing":     {args: []string{"install", "--routerd", filepath.Join(dir, "absent")}, message: "routerd executable not found; pass --routerd with an absolute path"},
		"directory":   {args: []string{"install", "--routerd", dir}, message: "routerd path must be an absolute path to a regular file"},
		"lookup fail": {args: []string{"install"}, lookupErr: errors.New(serviceSecretSentinel), message: "routerd executable not found; pass --routerd with an absolute path"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newServiceHarness(t)
			h.lookupErr = tc.lookupErr
			code, stdout, stderr := runServiceCLI(t, h, testServiceOptions(t), tc.args...)
			if code != ExitValidation {
				t.Fatalf("code = %d, want %d (stderr %q)", code, ExitValidation, stderr)
			}
			if stdout != "" || stderr != "ant-router: "+tc.message+"\n" {
				t.Fatalf("stdout = %q stderr = %q", stdout, stderr)
			}
			if h.manager.installed != nil {
				t.Fatal("Install must not be called for an invalid routerd")
			}
		})
	}
}

func TestServiceActionsTargetInstanceLabel(t *testing.T) {
	for _, action := range []string{"uninstall", "start", "stop", "status"} {
		t.Run(action, func(t *testing.T) {
			h := newServiceHarness(t)
			h.manager.status = service.Status{State: service.StateStopped}
			code, stdout, stderr := runServiceCLI(t, h, testServiceOptions(t), action)
			if code != ExitSuccess || stderr != "" {
				t.Fatalf("code = %d stderr = %q", code, stderr)
			}
			wantCalls := []string{action, "status"}
			if action == "status" {
				wantCalls = []string{"status"}
			}
			if !slices.Equal(h.manager.calls, wantCalls) {
				t.Fatalf("calls = %q, want %q", h.manager.calls, wantCalls)
			}
			for _, label := range h.manager.labels {
				if label != "dev.antrouter.routerd.work" {
					t.Fatalf("label = %q", label)
				}
			}
			if want := "service " + action + ": dev.antrouter.routerd.work stopped\n"; stdout != want {
				t.Fatalf("stdout = %q, want %q", stdout, want)
			}
		})
	}
}

func TestServiceErrorsAreMappedAndRedacted(t *testing.T) {
	secret := errors.New(serviceSecretSentinel)
	cases := map[string]struct {
		configure func(*serviceHarness)
		action    string
		exit      int
		code      string
		message   string
	}{
		"not owned": {
			configure: func(h *serviceHarness) {
				h.manager.installErr = service.ErrNotOwned{Path: "/x/" + serviceSecretSentinel}
			},
			action: "install", exit: ExitConflict, code: "conflict", message: "an existing service definition is not owned by ant-router",
		},
		"unsupported platform": {
			configure: func(h *serviceHarness) { h.managerErr = service.ErrUnsupportedPlatform{GOOS: "plan9"} },
			action:    "status", exit: ExitUnavailable, code: "unavailable", message: "service management is not supported on this platform",
		},
		"backend start failure": {
			configure: func(h *serviceHarness) { h.manager.startErr = secret },
			action:    "start", exit: ExitInternal, code: "internal", message: "service operation failed",
		},
		"backend status failure": {
			configure: func(h *serviceHarness) { h.manager.statusErr = secret },
			action:    "status", exit: ExitInternal, code: "internal", message: "service operation failed",
		},
		"manager construction failure": {
			configure: func(h *serviceHarness) { h.managerErr = secret },
			action:    "stop", exit: ExitInternal, code: "internal", message: "service operation failed",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for _, jsonMode := range []bool{false, true} {
				h := newServiceHarness(t)
				tc.configure(h)
				options := testServiceOptions(t)
				options.JSON = jsonMode
				args := []string{tc.action}
				if tc.action == "install" {
					args = append(args, "--routerd", h.routerd)
				}
				code, stdout, stderr := runServiceCLI(t, h, options, args...)
				if code != tc.exit {
					t.Fatalf("json=%v code = %d, want %d", jsonMode, code, tc.exit)
				}
				if stdout != "" {
					t.Fatalf("json=%v errors must not be written to stdout: %q", jsonMode, stdout)
				}
				if strings.Contains(stderr, serviceSecretSentinel) {
					t.Fatalf("json=%v stderr leaked backend detail: %q", jsonMode, stderr)
				}
				if !jsonMode {
					if stderr != "ant-router: "+tc.message+"\n" {
						t.Fatalf("stderr = %q", stderr)
					}
					continue
				}
				var envelope cliErrorEnvelope
				if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
					t.Fatalf("decode %q: %v", stderr, err)
				}
				if envelope.Error.ExitCode != tc.exit || envelope.Error.Code != tc.code || envelope.Error.Message != tc.message {
					t.Fatalf("envelope = %+v", envelope.Error)
				}
			}
		})
	}
}

func TestServiceInstallTightensExistingLogDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	h := newServiceHarness(t)
	options := testServiceOptions(t)
	paths, err := resolveCLIPaths(options)
	if err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(paths.InstanceDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runServiceCLI(t, h, options, "install", "--routerd", h.routerd); code != ExitSuccess {
		t.Fatalf("code = %d stderr = %q", code, stderr)
	}
	info, err := os.Stat(logDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("existing log dir mode = %#o, want 0700", info.Mode().Perm())
	}
}

func TestServiceInstallRejectsSymlinkedLogDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	h := newServiceHarness(t)
	options := testServiceOptions(t)
	paths, err := resolveCLIPaths(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.InstanceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(paths.InstanceDir, "logs")); err != nil {
		t.Fatal(err)
	}
	code, _, _ := runServiceCLI(t, h, options, "install", "--routerd", h.routerd)
	if code != ExitInternal || h.manager.installed != nil {
		t.Fatalf("code = %d installed = %v; a symlinked log dir must be refused", code, h.manager.installed != nil)
	}
}
