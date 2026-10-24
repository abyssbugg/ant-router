package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
	"github.com/abyssbugg/ant-router/internal/filelock"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
	"github.com/abyssbugg/ant-router/internal/service"
)

// serviceOperationTimeout bounds one service command, so a hung launchctl,
// systemctl, or PowerShell cannot block routerctl indefinitely. Install and
// reinstall on Task Scheduler take a few seconds; rollbacks run on their own
// detached budget inside the backends.
const serviceOperationTimeout = 2 * time.Minute

var (
	errRouterdNotFound     = errors.New("routerd executable not found")
	errRouterdInvalid      = errors.New("routerd executable is invalid")
	errRouterdInaccessible = errors.New("routerd executable cannot be inspected")
	errServiceOperation    = errors.New("service operation failed")
	errServiceTimeout      = errors.New("service operation timed out")
	errServiceInterrupted  = errors.New("service operation interrupted")
	errServiceOtherRoot    = errors.New("service is installed for another state directory")
	errServiceLogDir       = errors.New("service log directory is unsafe")
)

// serviceDeps holds the injectable collaborators of the service and doctor
// commands so tests never touch the host service manager or filesystem layout.
type serviceDeps struct {
	newManager    func(stateDir string) (service.Manager, error)
	lookupRouterd func() (string, error)
	// serviceDir returns the per-user directory holding service operation
	// locks, backend state (Task Scheduler locks), and the state-root record
	// of each installed instance. It is shared by every state root, because
	// a service label is per instance, not per state root.
	serviceDir func() (string, error)
	// timeout bounds one service command; zero means serviceOperationTimeout.
	timeout time.Duration
	// probeDataPlane checks that the data plane accepts connections.
	probeDataPlane func(ctx context.Context, baseURL string) error
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
		lookupRouterd:  siblingRouterd,
		serviceDir:     userServiceDir,
		probeDataPlane: dialDataPlane,
	}
}

// userServiceDir is ant-router's per-user service directory.
func userServiceDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ant-router", "service"), nil
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
	switch command.Action {
	case "install", "uninstall", "start", "stop", "status":
	default:
		return serviceResult{}, errUsage
	}
	paths, err := resolveCLIPaths(options)
	if err != nil {
		return serviceResult{}, err
	}
	label, err := service.DaemonLabel(paths.Instance.String())
	if err != nil {
		return serviceResult{}, classifyServiceError(err)
	}
	timeout := deps.timeout
	if timeout <= 0 {
		timeout = serviceOperationTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dir, err := deps.serviceDir()
	if err != nil {
		return serviceResult{}, errors.Join(errServiceOperation, err)
	}
	manager, err := deps.newManager(dir)
	if err != nil {
		return serviceResult{}, classifyServiceError(err)
	}
	// One routerctl service command per label at a time, across processes
	// and state roots, so the status reported below is the result of this
	// command and not of a concurrent one. The backends hold their own,
	// separate per-label locks inside each operation.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return serviceResult{}, errors.Join(errServiceOperation, err)
	}
	lock, err := filelock.Acquire(ctx, filepath.Join(dir, "."+label+".operation.lock"))
	if err != nil {
		return serviceResult{}, classifyServiceError(err)
	}
	defer func() { _ = lock.Release() }()

	if err := checkServiceStateRoot(ctx, manager, dir, label, paths.StateRoot); err != nil {
		return serviceResult{}, err
	}
	switch command.Action {
	case "install":
		spec, err := buildDaemonSpec(paths, command, deps)
		if err != nil {
			return serviceResult{}, err
		}
		// Recorded before installing: a stale record is harmless (it is
		// ignored once the service is gone), a missing one is not.
		if err := writeServiceStateRoot(ctx, dir, label, paths.StateRoot); err != nil {
			return serviceResult{}, errors.Join(errServiceOperation, err)
		}
		if err := manager.Install(ctx, spec); err != nil {
			return serviceResult{}, classifyServiceError(err)
		}
	case "uninstall":
		if err := manager.Uninstall(ctx, label); err != nil {
			return serviceResult{}, classifyServiceError(err)
		}
		// The record is self-healing (ignored once the service is gone), so
		// failing to remove it does not fail a completed uninstall.
		_, _ = atomicfile.Remove(ctx, serviceStateRootRecord(dir, label))
	case "start":
		if err := manager.Start(ctx, label); err != nil {
			return serviceResult{}, classifyServiceError(err)
		}
	case "stop":
		if err := manager.Stop(ctx, label); err != nil {
			return serviceResult{}, classifyServiceError(err)
		}
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

func serviceStateRootRecord(dir, label string) string {
	return filepath.Join(dir, label+".state-root")
}

func writeServiceStateRoot(ctx context.Context, dir, label, stateRoot string) error {
	_, err := atomicfile.Write(ctx, serviceStateRootRecord(dir, label), []byte(filepath.Clean(stateRoot)+"\n"), 0o600)
	return err
}

// checkServiceStateRoot refuses to manage a label whose service was
// installed from a different state root. The label (and the daemon's
// control endpoint) is per instance, so two state roots cannot both own one;
// the second would silently take over or remove the first one's service.
// A record whose service no longer exists, or is not ours, is stale.
func checkServiceStateRoot(ctx context.Context, manager service.Manager, dir, label, stateRoot string) error {
	data, err := os.ReadFile(serviceStateRootRecord(dir, label))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.Join(errServiceOperation, err)
	}
	if sameStatePath(strings.TrimSpace(string(data)), stateRoot) {
		return nil
	}
	status, err := manager.Status(ctx, label)
	var notOwned service.ErrNotOwned
	switch {
	case errors.As(err, &notOwned):
		return nil
	case err != nil:
		return classifyServiceError(err)
	case status.State == service.StateNotInstalled:
		return nil
	}
	return errServiceOtherRoot
}

func sameStatePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
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
	if err := validateRouterd(executable); err != nil {
		return service.Spec{}, err
	}
	logDir := filepath.Join(paths.InstanceDir, "logs")
	if err := ensurePrivateLogDir(paths.StateRoot, logDir); err != nil {
		return service.Spec{}, errors.Join(errServiceLogDir, err)
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

// validateRouterd checks that path can be installed as the service
// program: absolute, an existing regular file, and (outside Windows)
// executable by its owner, which is what service.Spec.Validate requires.
func validateRouterd(path string) error {
	if !filepath.IsAbs(path) {
		return errors.Join(errRouterdInvalid, service.ErrRelativePath{Path: path})
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.Join(errRouterdNotFound, err)
	case err != nil:
		return errors.Join(errRouterdInaccessible, err)
	case !info.Mode().IsRegular():
		return errors.Join(errRouterdInvalid, service.ErrNotRegularFile{Path: path})
	case runtime.GOOS != "windows" && info.Mode()&0o100 == 0:
		return errors.Join(errRouterdInvalid, fmt.Errorf("%s is not executable", path))
	}
	return nil
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
	return filepath.Join(filepath.Dir(executable), name), nil
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
// echoes backend command output. Context expiry becomes a service timeout or
// interruption rather than a daemon connection failure.
func classifyServiceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return errors.Join(errServiceTimeout, err)
	case errors.Is(err, context.Canceled):
		return errors.Join(errServiceInterrupted, err)
	}
	if _, ok := lookupServiceError(err); ok {
		return err
	}
	return errors.Join(errServiceOperation, err)
}

// serviceErrorKind is one row of the service error table: the single source
// of both the exit code and the fixed, secret-free message.
type serviceErrorKind struct {
	matches func(error) bool
	exit    int
	message string
}

func errorIs(target error) func(error) bool {
	return func(err error) bool { return errors.Is(err, target) }
}

func errorAs[T error]() func(error) bool {
	return func(err error) bool {
		var target T
		return errors.As(err, &target)
	}
}

// serviceErrors is ordered: routerd-specific sentinels come before the
// generic typed errors they wrap, and errServiceOperation is last.
var serviceErrors = []serviceErrorKind{
	{errorIs(errServiceTimeout), ExitUnavailable, "service manager did not respond in time"},
	{errorIs(errServiceInterrupted), ExitUnavailable, "service operation was interrupted"},
	{errorAs[service.ErrNotOwned](), ExitConflict, "an existing service definition is not owned by ant-router"},
	{errorAs[service.ErrLabelInUse](), ExitConflict, "the service label is already used by another service definition"},
	{errorIs(errServiceOtherRoot), ExitConflict, "this instance's service is installed for a different state directory; manage it from there or uninstall it first"},
	{errorAs[service.ErrNotInstalled](), ExitConflict, "service is not installed; run routerctl service install"},
	{errorIs(errRouterdNotFound), ExitValidation, "routerd executable not found; pass --routerd with an absolute path"},
	{errorIs(errRouterdInvalid), ExitValidation, "routerd path must be an absolute path to an executable regular file"},
	{errorIs(errRouterdInaccessible), ExitValidation, "routerd executable cannot be inspected"},
	{errorIs(errServiceLogDir), ExitConflict, "service log directory must be a private real directory inside the state directory"},
	{errorAs[service.ErrInvalidLabel](), ExitValidation, "service label is invalid"},
	{errorAs[service.ErrRelativePath](), ExitValidation, "service paths must be absolute"},
	{errorAs[service.ErrNotRegularFile](), ExitConflict, "an existing service definition is not a regular file"},
	{errorAs[service.ErrUnsupportedLogPath](), ExitValidation, "service log path is not supported by this service manager"},
	{errorAs[service.ErrUnsupportedSystemd](), ExitUnavailable, "the installed systemd is too old for user service management"},
	{errorAs[service.ErrUnsupportedPlatform](), ExitUnavailable, "service management is not supported on this platform"},
	{errorIs(errServiceOperation), ExitInternal, "service operation failed"},
}

func lookupServiceError(err error) (serviceErrorKind, bool) {
	for _, kind := range serviceErrors {
		if kind.matches(err) {
			return kind, true
		}
	}
	return serviceErrorKind{}, false
}

// serviceErrorExit maps typed service errors to CLI exit codes.
func serviceErrorExit(err error) (int, bool) {
	kind, ok := lookupServiceError(err)
	return kind.exit, ok
}

// serviceErrorMessage returns a fixed, secret-free message for service errors.
func serviceErrorMessage(err error) (string, bool) {
	kind, ok := lookupServiceError(err)
	return kind.message, ok
}
