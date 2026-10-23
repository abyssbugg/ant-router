package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/abyssbugg/ant-router/internal/runtimepaths"
	"github.com/abyssbugg/ant-router/internal/service"
)

var (
	errRouterdNotFound  = errors.New("routerd executable not found")
	errServiceOperation = errors.New("service operation failed")
)

// serviceDeps holds the injectable collaborators of the service and doctor
// commands so tests never touch the host service manager or filesystem layout.
type serviceDeps struct {
	newManager    func(stateDir string) (service.Manager, error)
	lookupRouterd func() (string, error)
}

func defaultServiceDeps() serviceDeps {
	return serviceDeps{
		newManager: func(stateDir string) (service.Manager, error) {
			platform, err := service.CurrentPlatform(stateDir)
			if err != nil {
				return nil, err
			}
			return service.NewManager(platform)
		},
		lookupRouterd: siblingRouterd,
	}
}

type serviceResult struct {
	SchemaVersion int    `json:"schema_version"`
	Action        string `json:"action"`
	Label         string `json:"label"`
	State         string `json:"state"`
	Detail        string `json:"detail,omitempty"`
}

func resolveCLIPaths(options cliOptions) (runtimepaths.Paths, error) {
	return runtimepaths.Resolve(runtimepaths.Options{
		InstanceOverride: options.Instance,
		StateOverride:    options.StateRoot,
		RuntimeOverride:  options.RuntimeRoot,
	})
}

func serviceStateDir(paths runtimepaths.Paths) string {
	return filepath.Join(paths.InstanceDir, "service")
}

// executeServiceCommand handles `routerctl service <install|uninstall|start|stop|status>`.
func executeServiceCommand(ctx context.Context, options cliOptions, command cliCommand, stdout, stderr io.Writer, deps serviceDeps) int {
	result, err := runServiceCommand(ctx, command, options, deps)
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	if err := writeServiceResult(stdout, options, result); err != nil {
		_ = writeCLIError(stderr, options, err)
		return ExitInternal
	}
	return ExitSuccess
}

func runServiceCommand(ctx context.Context, command cliCommand, options cliOptions, deps serviceDeps) (serviceResult, error) {
	paths, err := resolveCLIPaths(options)
	if err != nil {
		return serviceResult{}, err
	}
	label, err := service.DaemonLabel(paths.Instance.String())
	if err != nil {
		return serviceResult{}, err
	}
	manager, err := deps.newManager(serviceStateDir(paths))
	if err != nil {
		return serviceResult{}, classifyServiceError(err)
	}
	switch command.Action {
	case "install":
		spec, err := buildDaemonSpec(paths, command, deps)
		if err != nil {
			return serviceResult{}, err
		}
		err = manager.Install(ctx, spec)
		if err != nil {
			return serviceResult{}, classifyServiceError(err)
		}
	case "uninstall":
		err = manager.Uninstall(ctx, label)
		if err != nil {
			return serviceResult{}, classifyServiceError(err)
		}
	case "start":
		err = manager.Start(ctx, label)
		if err != nil {
			return serviceResult{}, classifyServiceError(err)
		}
	case "stop":
		err = manager.Stop(ctx, label)
		if err != nil {
			return serviceResult{}, classifyServiceError(err)
		}
	case "status":
	default:
		return serviceResult{}, errUsage
	}
	status, err := manager.Status(ctx, label)
	if err != nil {
		return serviceResult{}, classifyServiceError(err)
	}
	return serviceResult{
		SchemaVersion: cliSchemaVersion,
		Action:        command.Action,
		Label:         label,
		State:         stateString(status.State),
		Detail:        status.Detail,
	}, nil
}

func buildDaemonSpec(paths runtimepaths.Paths, command cliCommand, deps serviceDeps) (service.Spec, error) {
	executable := ""
	if len(command.Args) == 1 {
		executable = command.Args[0]
	} else {
		var err error
		executable, err = deps.lookupRouterd()
		if err != nil {
			return service.Spec{}, errors.Join(errRouterdNotFound, err)
		}
	}
	if !filepath.IsAbs(executable) {
		return service.Spec{}, service.ErrRelativePath{Path: executable}
	}
	info, err := os.Stat(executable)
	if err != nil {
		return service.Spec{}, errors.Join(errRouterdNotFound, err)
	}
	if !info.Mode().IsRegular() {
		return service.Spec{}, service.ErrNotRegularFile{Path: executable}
	}
	logDir := filepath.Join(paths.InstanceDir, "logs")
	if err := ensurePrivateDir(logDir); err != nil {
		return service.Spec{}, errors.Join(errServiceOperation, err)
	}
	spec, err := service.DaemonSpec(service.DaemonOptions{
		Instance:    paths.Instance.String(),
		Executable:  executable,
		StateRoot:   paths.StateRoot,
		RuntimeRoot: paths.RuntimeRoot,
		LogPath:     filepath.Join(logDir, "routerd.log"),
	})
	if err != nil {
		return service.Spec{}, classifyServiceError(err)
	}
	return spec, nil
}

func writeServiceResult(writer io.Writer, options cliOptions, result serviceResult) error {
	if options.JSON {
		return writeJSONOutput(writer, result)
	}
	_, err := fmt.Fprintf(writer, "service %s: %s %s\n", result.Action, result.Label, result.State)
	return err
}

// ensurePrivateDir creates dir with mode 0700, or tightens an existing real
// directory to 0700. A symlink or non-directory at dir is refused so service
// logs are never redirected outside the instance state directory.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if runtime.GOOS == "windows" || info.Mode().Perm() == 0o700 {
		return nil
	}
	return os.Chmod(dir, 0o700)
}

// siblingRouterd finds routerd next to the running routerctl executable.
func siblingRouterd() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	name := "routerd"
	if runtime.GOOS == "windows" {
		name = "routerd.exe"
	}
	candidate := filepath.Join(filepath.Dir(executable), name)
	info, err := os.Stat(candidate)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", candidate)
	}
	return candidate, nil
}

func stateString(state service.State) string {
	switch state {
	case service.StateNotInstalled:
		return "not_installed"
	case service.StateStopped:
		return "stopped"
	case service.StateRunning:
		return "running"
	default:
		return "unknown"
	}
}

// classifyServiceError preserves typed service errors and marks every other
// backend failure as a generic service operation failure, so CLI output never
// echoes backend command output.
func classifyServiceError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := serviceErrorExit(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.Join(errServiceOperation, err)
}

// serviceErrorExit maps typed service errors to CLI exit codes.
func serviceErrorExit(err error) (int, bool) {
	var notOwned service.ErrNotOwned
	var invalidLabel service.ErrInvalidLabel
	var relativePath service.ErrRelativePath
	var notRegular service.ErrNotRegularFile
	var logPath service.ErrUnsupportedLogPath
	var unsupported service.ErrUnsupportedPlatform
	switch {
	case errors.As(err, &notOwned):
		return ExitConflict, true
	case errors.Is(err, errRouterdNotFound), errors.As(err, &invalidLabel), errors.As(err, &relativePath),
		errors.As(err, &notRegular), errors.As(err, &logPath):
		return ExitValidation, true
	case errors.As(err, &unsupported):
		return ExitUnavailable, true
	case errors.Is(err, errServiceOperation):
		return ExitInternal, true
	}
	return 0, false
}

// serviceErrorMessage returns a fixed, secret-free message for service errors.
func serviceErrorMessage(err error) (string, bool) {
	var notOwned service.ErrNotOwned
	var relativePath service.ErrRelativePath
	var notRegular service.ErrNotRegularFile
	var invalidLabel service.ErrInvalidLabel
	var logPath service.ErrUnsupportedLogPath
	var unsupported service.ErrUnsupportedPlatform
	switch {
	case errors.As(err, &notOwned):
		return "an existing service definition is not owned by ant-router", true
	case errors.Is(err, errRouterdNotFound):
		return "routerd executable not found; pass --routerd with an absolute path", true
	case errors.As(err, &relativePath), errors.As(err, &notRegular):
		return "routerd path must be an absolute path to a regular file", true
	case errors.As(err, &invalidLabel):
		return "service label is invalid", true
	case errors.As(err, &logPath):
		return "service log path is not supported by this service manager", true
	case errors.As(err, &unsupported):
		return "service management is not supported on this platform", true
	case errors.Is(err, errServiceOperation):
		return "service operation failed", true
	}
	return "", false
}
