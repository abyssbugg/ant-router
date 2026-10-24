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

func TestDaemonLabel(t *testing.T) {
	for _, tc := range []struct {
		instance string
		want     string
		wantErr  bool
	}{
		{"", "dev.antrouter.routerd", false},
		{"default", "dev.antrouter.routerd", false},
		{"work", "dev.antrouter.routerd.work", false},
		{"a-1", "dev.antrouter.routerd.a-1", false},
		{"Bad", "", true},
		{"../x", "", true},
		{"a b", "", true},
		{strings.Repeat("a", 33), "", true},
	} {
		got, err := DaemonLabel(tc.instance)
		if (err != nil) != tc.wantErr {
			t.Fatalf("DaemonLabel(%q) err = %v, wantErr %v", tc.instance, err, tc.wantErr)
		}
		if got != tc.want {
			t.Fatalf("DaemonLabel(%q) = %q, want %q", tc.instance, got, tc.want)
		}
		if err == nil {
			if verr := ValidateLabel(got); verr != nil {
				t.Fatalf("DaemonLabel(%q) produced invalid label: %v", tc.instance, verr)
			}
		}
	}
}

func TestDaemonSpec(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "routerd")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := DaemonOptions{
		Instance:    "work",
		Executable:  exe,
		StateRoot:   filepath.Join(dir, "state root"),
		RuntimeRoot: filepath.Join(dir, "run"),
		LogPath:     filepath.Join(dir, "logs", "routerd.log"),
	}
	spec, err := DaemonSpec(opts)
	if err != nil {
		t.Fatalf("DaemonSpec: %v", err)
	}
	want := Spec{
		Label:      "dev.antrouter.routerd.work",
		Executable: exe,
		Args:       []string{"--instance", "work", "--state-dir", opts.StateRoot, "--runtime-dir", opts.RuntimeRoot},
		LogPath:    opts.LogPath,
	}
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("DaemonSpec = %+v, want %+v", spec, want)
	}

	// RuntimeRoot is empty on Windows; the flag must then be omitted.
	opts.RuntimeRoot = ""
	opts.Instance = ""
	spec, err = DaemonSpec(opts)
	if err != nil {
		t.Fatalf("DaemonSpec without runtime root: %v", err)
	}
	if want := []string{"--instance", "default", "--state-dir", opts.StateRoot}; !reflect.DeepEqual(spec.Args, want) {
		t.Fatalf("Args = %q, want %q", spec.Args, want)
	}
	if spec.Label != "dev.antrouter.routerd" {
		t.Fatalf("Label = %q", spec.Label)
	}
}

func TestDaemonSpecRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "routerd")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := DaemonOptions{Instance: "work", Executable: exe, StateRoot: dir, LogPath: filepath.Join(dir, "r.log")}
	for name, mutate := range map[string]func(*DaemonOptions){
		"bad instance":      func(o *DaemonOptions) { o.Instance = "Bad" },
		"relative state":    func(o *DaemonOptions) { o.StateRoot = "state" },
		"empty state":       func(o *DaemonOptions) { o.StateRoot = "" },
		"relative runtime":  func(o *DaemonOptions) { o.RuntimeRoot = "run" },
		"missing exe":       func(o *DaemonOptions) { o.Executable = filepath.Join(dir, "nope") },
		"relative log path": func(o *DaemonOptions) { o.LogPath = "r.log" },
	} {
		t.Run(name, func(t *testing.T) {
			o := base
			mutate(&o)
			if _, err := DaemonSpec(o); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNewManagerSelectsBackend(t *testing.T) {
	runner := fakeRunnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) { return nil, nil, nil })
	// Paths must be absolute on the host running the test (a Unix literal
	// such as "/home/u" is not absolute on Windows).
	root := t.TempDir()
	p := Platform{Home: filepath.Join(root, "home"), ConfigDir: filepath.Join(root, "home", ".config"), StateDir: filepath.Join(root, "state"), UID: 501, Runner: runner}
	for goos, want := range map[string]any{
		"darwin":  &LaunchdBackend{},
		"linux":   &SystemdBackend{},
		"windows": &TaskSchedulerBackend{},
	} {
		p.GOOS = goos
		m, err := NewManager(p)
		if err != nil {
			t.Fatalf("NewManager(%s): %v", goos, err)
		}
		if reflect.TypeOf(m) != reflect.TypeOf(want) {
			t.Fatalf("NewManager(%s) = %T, want %T", goos, m, want)
		}
		// The backend must be wired to this platform's directories and
		// runner, not merely be of the right type.
		sameRunner := func(got Runner) bool {
			return reflect.ValueOf(got).Pointer() == reflect.ValueOf(runner).Pointer()
		}
		switch b := m.(type) {
		case *LaunchdBackend:
			if b.home != p.Home || b.uid != p.UID || !sameRunner(b.runner) {
				t.Fatalf("launchd wiring = home %q uid %d", b.home, b.uid)
			}
		case *SystemdBackend:
			if b.configDir != p.ConfigDir || !sameRunner(b.runner) {
				t.Fatalf("systemd wiring = configDir %q", b.configDir)
			}
		case *TaskSchedulerBackend:
			if b.stateDir != p.StateDir || !sameRunner(b.runner) || b.currentUserSID == nil {
				t.Fatalf("task scheduler wiring = stateDir %q", b.stateDir)
			}
		}
	}
	p.GOOS = "plan9"
	var unsupported ErrUnsupportedPlatform
	if _, err := NewManager(p); !errors.As(err, &unsupported) || unsupported.GOOS != "plan9" {
		t.Fatalf("NewManager(plan9) err = %v, want ErrUnsupportedPlatform", err)
	}
}

func TestNewManagerRequiresPlatformFields(t *testing.T) {
	runner := fakeRunnerFunc(nil)
	abs := t.TempDir()
	for name, p := range map[string]Platform{
		"darwin no home":       {GOOS: "darwin", UID: 501, Runner: runner},
		"darwin relative home": {GOOS: "darwin", Home: "home", UID: 501, Runner: runner},
		"darwin negative uid":  {GOOS: "darwin", Home: abs, UID: -1, Runner: runner},
		"linux no config dir":  {GOOS: "linux", Runner: runner},
		"windows no state dir": {GOOS: "windows", Runner: runner},
		"nil runner":           {GOOS: "linux", ConfigDir: abs},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewManager(p); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
