package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/abyssbugg/ant-router/internal/controltransport"
)

// DaemonLabelPrefix is the service label of the default routerd instance.
// Named instances append ".<instance>".
const DaemonLabelPrefix = "dev.antrouter.routerd"

// ErrUnsupportedPlatform is returned when no service backend exists for GOOS.
type ErrUnsupportedPlatform struct {
	GOOS string
}

func (e ErrUnsupportedPlatform) Error() string {
	return fmt.Sprintf("per-user services are not supported on %s", e.GOOS)
}

// DaemonLabel returns the service label for a routerd instance. An empty
// instance means the default instance.
func DaemonLabel(instance string) (string, error) {
	name, err := parseInstance(instance)
	if err != nil {
		return "", err
	}
	if name == controltransport.DefaultInstance {
		return DaemonLabelPrefix, nil
	}
	label := DaemonLabelPrefix + "." + name.String()
	if err := ValidateLabel(label); err != nil {
		return "", err
	}
	return label, nil
}

// DaemonOptions describes the routerd instance a service runs.
type DaemonOptions struct {
	// Instance is the routerd instance name; empty means the default.
	Instance string
	// Executable is the absolute path of the routerd binary.
	Executable string
	// StateRoot and RuntimeRoot are passed to routerd as --state-dir and
	// --runtime-dir. An empty RuntimeRoot omits --runtime-dir so routerd
	// uses its own default; that is the normal case on Windows, where
	// runtimepaths has no default runtime root. A non-empty RuntimeRoot is
	// passed through on every platform.
	StateRoot   string
	RuntimeRoot string
	// LogPath receives daemon output: launchd and systemd redirect to it;
	// Task Scheduler passes it to routerd as --log-file.
	LogPath string
}

// DaemonSpec builds and validates the service Spec for a routerd instance.
// Paths are passed explicitly so the service never depends on the
// environment of the service manager.
func DaemonSpec(options DaemonOptions) (Spec, error) {
	name, err := parseInstance(options.Instance)
	if err != nil {
		return Spec{}, err
	}
	label, err := DaemonLabel(name.String())
	if err != nil {
		return Spec{}, err
	}
	if options.StateRoot == "" || !filepath.IsAbs(options.StateRoot) {
		return Spec{}, ErrRelativePath{Path: options.StateRoot}
	}
	args := []string{"--instance", name.String(), "--state-dir", options.StateRoot}
	if options.RuntimeRoot != "" {
		if !filepath.IsAbs(options.RuntimeRoot) {
			return Spec{}, ErrRelativePath{Path: options.RuntimeRoot}
		}
		args = append(args, "--runtime-dir", options.RuntimeRoot)
	}
	spec := Spec{
		Label:      label,
		Executable: options.Executable,
		Args:       args,
		LogPath:    options.LogPath,
	}
	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func parseInstance(instance string) (controltransport.InstanceName, error) {
	if instance == "" {
		return controltransport.DefaultInstance, nil
	}
	name, err := controltransport.ParseInstanceName(instance)
	if err != nil {
		return "", fmt.Errorf("invalid instance %q: %w", instance, err)
	}
	return name, nil
}

// Platform holds the per-user locations and runner a backend needs.
type Platform struct {
	GOOS string
	// Home is the user's home directory (launchd).
	Home string
	// ConfigDir is the user's configuration directory, e.g. ~/.config (systemd).
	ConfigDir string
	// StateDir holds backend-owned state such as per-label lock files (Task
	// Scheduler); task ownership itself is read from the registered task.
	StateDir string
	// UID is the user's numeric ID (launchd gui/<uid> domain).
	UID    int
	Runner Runner
}

// CurrentPlatform describes the running user's platform. stateDir is the
// directory where backend-owned records are stored.
func CurrentPlatform(stateDir string) (Platform, error) {
	p := Platform{GOOS: runtime.GOOS, StateDir: stateDir, UID: os.Getuid(), Runner: &ExecRunner{}}
	var err error
	switch p.GOOS {
	case "darwin":
		if p.Home, err = os.UserHomeDir(); err != nil {
			return Platform{}, fmt.Errorf("resolve home directory: %w", err)
		}
	case "linux":
		// systemd --user reads units from $XDG_CONFIG_HOME/systemd/user.
		if p.ConfigDir, err = os.UserConfigDir(); err != nil {
			return Platform{}, fmt.Errorf("resolve config directory: %w", err)
		}
	}
	return p, nil
}

// NewManager returns the service backend for p.GOOS.
func NewManager(p Platform) (Manager, error) {
	if p.Runner == nil {
		return nil, errors.New("service platform has no command runner")
	}
	requireAbs := func(name, path string) error {
		if path == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("service platform %s must be an absolute path: %w", name, ErrRelativePath{Path: path})
		}
		return nil
	}
	switch p.GOOS {
	case "darwin":
		if err := requireAbs("home", p.Home); err != nil {
			return nil, err
		}
		if p.UID < 0 {
			return nil, fmt.Errorf("service platform uid %d is invalid", p.UID)
		}
		return NewLaunchdBackend(p.Home, p.UID, p.Runner), nil
	case "linux":
		if err := requireAbs("config directory", p.ConfigDir); err != nil {
			return nil, err
		}
		return NewSystemdBackend(p.ConfigDir, p.Runner), nil
	case "windows":
		if err := requireAbs("state directory", p.StateDir); err != nil {
			return nil, err
		}
		return NewTaskSchedulerBackend(p.StateDir, p.Runner), nil
	default:
		return nil, ErrUnsupportedPlatform{GOOS: p.GOOS}
	}
}
