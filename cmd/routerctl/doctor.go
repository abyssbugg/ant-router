package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
	"github.com/abyssbugg/ant-router/internal/service"
)

// doctorTimeout bounds each doctor probe; a variable so tests can shorten it.
var doctorTimeout = 3 * time.Second

const (
	doctorOK   = "ok"
	doctorWarn = "warn"
	doctorFail = "fail"
	doctorSkip = "skip"
)

// doctorCheck is one diagnostic result. Messages are fixed text or local
// paths; they never contain token material, configuration contents, or raw
// backend output.
type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type doctorReport struct {
	SchemaVersion int           `json:"schema_version"`
	Healthy       bool          `json:"healthy"`
	Checks        []doctorCheck `json:"checks"`
}

// executeDoctor runs local and daemon diagnostics. It exits ExitSuccess when
// no check failed and ExitUnavailable otherwise.
func executeDoctor(ctx context.Context, options cliOptions, stdout, stderr io.Writer, factory clientFactory, deps serviceDeps) int {
	report := runDoctor(ctx, options, factory, deps)
	if err := writeDoctorReport(stdout, options, report); err != nil {
		_ = writeCLIError(stderr, options, err)
		return ExitInternal
	}
	if !report.Healthy {
		return ExitUnavailable
	}
	return ExitSuccess
}

func runDoctor(ctx context.Context, options cliOptions, factory clientFactory, deps serviceDeps) doctorReport {
	var checks []doctorCheck
	paths, err := resolveCLIPaths(options)
	if err != nil {
		checks = append(checks, doctorCheck{Name: "paths", Status: doctorFail, Message: "cannot resolve runtime paths"})
		return newDoctorReport(checks)
	}
	checks = append(checks,
		doctorCheck{Name: "paths", Status: doctorOK, Message: paths.InstanceDir},
		doctorRouterdCheck(deps),
		doctorTokenCheck(paths),
		doctorServiceCheck(ctx, paths, deps),
	)
	checks = append(checks, doctorDaemonChecks(ctx, options, factory, deps)...)
	return newDoctorReport(checks)
}

func newDoctorReport(checks []doctorCheck) doctorReport {
	healthy := true
	for _, check := range checks {
		if check.Status == doctorFail {
			healthy = false
		}
	}
	return doctorReport{SchemaVersion: cliSchemaVersion, Healthy: healthy, Checks: checks}
}

// doctorRouterdCheck applies the same validation as service install, so a
// binary that install would refuse is never reported as ok.
func doctorRouterdCheck(deps serviceDeps) doctorCheck {
	path, err := deps.lookupRouterd()
	if err != nil {
		return doctorCheck{Name: "routerd_binary", Status: doctorWarn, Message: "routerd not found next to routerctl"}
	}
	switch err := validateRouterd(path); {
	case errors.Is(err, errRouterdNotFound):
		return doctorCheck{Name: "routerd_binary", Status: doctorWarn, Message: "routerd not found next to routerctl"}
	case errors.Is(err, errRouterdInaccessible):
		return doctorCheck{Name: "routerd_binary", Status: doctorFail, Message: "routerd cannot be inspected"}
	case err != nil:
		return doctorCheck{Name: "routerd_binary", Status: doctorFail, Message: "routerd is not an executable regular file"}
	}
	return doctorCheck{Name: "routerd_binary", Status: doctorOK, Message: path}
}

func doctorTokenCheck(paths runtimepaths.Paths) doctorCheck {
	info, err := os.Lstat(paths.TokenPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return doctorCheck{Name: "control_token", Status: doctorFail, Message: "control token not found; start routerd"}
	case err != nil:
		return doctorCheck{Name: "control_token", Status: doctorFail, Message: "control token cannot be inspected"}
	case !info.Mode().IsRegular():
		return doctorCheck{Name: "control_token", Status: doctorFail, Message: "control token is not a regular file"}
	case runtime.GOOS != "windows" && info.Mode().Perm() != 0o600:
		// The token store loads only a 0600 token, so any other mode is a
		// token the daemon and routerctl will refuse.
		return doctorCheck{Name: "control_token", Status: doctorFail, Message: fmt.Sprintf("control token mode is %#o; it must be 0600", info.Mode().Perm())}
	}
	return doctorCheck{Name: "control_token", Status: doctorOK, Message: paths.TokenPath}
}

func doctorServiceCheck(ctx context.Context, paths runtimepaths.Paths, deps serviceDeps) doctorCheck {
	label, err := service.DaemonLabel(paths.Instance.String())
	if err != nil {
		return doctorCheck{Name: "service", Status: doctorFail, Message: "service label is invalid"}
	}
	dir, err := deps.serviceDir()
	if err != nil {
		return doctorCheck{Name: "service", Status: doctorFail, Message: "service manager unavailable"}
	}
	manager, err := deps.newManager(dir)
	if err != nil {
		var unsupported service.ErrUnsupportedPlatform
		if errors.As(err, &unsupported) {
			return doctorCheck{Name: "service", Status: doctorSkip, Message: "service management is not supported on this platform"}
		}
		return doctorCheck{Name: "service", Status: doctorFail, Message: "service manager unavailable"}
	}
	serviceCtx, cancel := context.WithTimeout(ctx, doctorTimeout)
	defer cancel()
	status, err := manager.Status(serviceCtx, label)
	if err != nil {
		return doctorCheck{Name: "service", Status: doctorFail, Message: "service status unavailable"}
	}
	switch status.State {
	case service.StateRunning:
		return doctorCheck{Name: "service", Status: doctorOK, Message: "running"}
	case service.StateStopped:
		return doctorCheck{Name: "service", Status: doctorWarn, Message: "installed but stopped"}
	case service.StateNotInstalled:
		return doctorCheck{Name: "service", Status: doctorWarn, Message: "not installed"}
	}
	return doctorCheck{Name: "service", Status: doctorWarn, Message: "state unknown"}
}

func doctorDaemonChecks(ctx context.Context, options cliOptions, factory clientFactory, deps serviceDeps) []doctorCheck {
	skipped := doctorCheck{Name: "data_plane", Status: doctorSkip, Message: "daemon unavailable"}
	client, err := factory(options)
	if err != nil {
		return []doctorCheck{{Name: "daemon", Status: doctorFail, Message: "cannot connect to daemon"}, skipped}
	}
	defer client.CloseIdleConnections()
	daemonCtx, cancel := context.WithTimeout(ctx, doctorTimeout)
	defer cancel()
	status, err := client.Status(daemonCtx)
	if err != nil {
		return []doctorCheck{{Name: "daemon", Status: doctorFail, Message: "daemon status unavailable"}, skipped}
	}
	cancel()
	// The probe gets its own budget: a status reply that arrives near its
	// deadline must not leave the probe an already-expired context.
	probeCtx, cancelProbe := context.WithTimeout(ctx, doctorTimeout)
	defer cancelProbe()
	return []doctorCheck{
		doctorDaemonStateCheck(status),
		doctorDataPlaneCheck(probeCtx, status.DataPlane, deps),
	}
}

// doctorDaemonStateCheck reports the daemon lifecycle state: a daemon that
// answers but is not serving is not healthy.
func doctorDaemonStateCheck(status control.StatusResponse) doctorCheck {
	uptime := time.Duration(status.UptimeSeconds) * time.Second
	switch status.DaemonState {
	case "running":
		return doctorCheck{Name: "daemon", Status: doctorOK, Message: fmt.Sprintf("running (uptime %s)", uptime)}
	case "starting", "draining":
		return doctorCheck{Name: "daemon", Status: doctorWarn, Message: status.DaemonState}
	case "stopped":
		return doctorCheck{Name: "daemon", Status: doctorFail, Message: "stopped"}
	}
	return doctorCheck{Name: "daemon", Status: doctorFail, Message: "daemon state unknown"}
}

// doctorDataPlaneCheck reports whether an enabled data plane actually
// accepts connections, not merely that it is configured.
func doctorDataPlaneCheck(ctx context.Context, summary *control.DataPlaneSummary, deps serviceDeps) doctorCheck {
	if summary == nil || !summary.Enabled {
		return doctorCheck{Name: "data_plane", Status: doctorWarn, Message: "disabled"}
	}
	if err := deps.probeDataPlane(ctx, summary.BaseURL); err != nil {
		return doctorCheck{Name: "data_plane", Status: doctorFail, Message: "data plane is not accepting connections"}
	}
	return doctorCheck{Name: "data_plane", Status: doctorOK, Message: summary.BaseURL}
}

// dialDataPlane opens, and immediately closes, a TCP connection to the data
// plane listener named by baseURL.
func dialDataPlane(ctx context.Context, baseURL string) error {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	if parsed.Hostname() == "" {
		return errors.New("data plane URL has no host")
	}
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return errors.New("data plane URL has no port")
		}
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return err
	}
	return conn.Close()
}

func writeDoctorReport(writer io.Writer, options cliOptions, report doctorReport) error {
	if options.JSON {
		return writeJSONOutput(writer, report)
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(writer, "[%s] %s: %s\n", check.Status, check.Name, check.Message); err != nil {
			return err
		}
	}
	return nil
}
