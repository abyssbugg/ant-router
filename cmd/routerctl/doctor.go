package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
	"github.com/abyssbugg/ant-router/internal/service"
)

const doctorTimeout = 3 * time.Second

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
	checks = append(checks, doctorDaemonChecks(ctx, options, factory)...)
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

func doctorRouterdCheck(deps serviceDeps) doctorCheck {
	path, err := deps.lookupRouterd()
	if err != nil {
		return doctorCheck{Name: "routerd_binary", Status: doctorWarn, Message: "routerd not found next to routerctl"}
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
	case runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0:
		return doctorCheck{Name: "control_token", Status: doctorWarn, Message: fmt.Sprintf("control token is accessible by group or others (mode %#o)", info.Mode().Perm())}
	}
	return doctorCheck{Name: "control_token", Status: doctorOK, Message: paths.TokenPath}
}

func doctorServiceCheck(ctx context.Context, paths runtimepaths.Paths, deps serviceDeps) doctorCheck {
	label, err := service.DaemonLabel(paths.Instance.String())
	if err != nil {
		return doctorCheck{Name: "service", Status: doctorFail, Message: "service label is invalid"}
	}
	manager, err := deps.newManager(serviceStateDir(paths))
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

func doctorDaemonChecks(ctx context.Context, options cliOptions, factory clientFactory) []doctorCheck {
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
	return []doctorCheck{
		{Name: "daemon", Status: doctorOK, Message: fmt.Sprintf("running (uptime %s)", time.Duration(status.UptimeSeconds)*time.Second)},
		doctorDataPlaneCheck(status.DataPlane),
	}
}

func doctorDataPlaneCheck(summary *control.DataPlaneSummary) doctorCheck {
	if summary == nil || !summary.Enabled {
		return doctorCheck{Name: "data_plane", Status: doctorWarn, Message: "disabled"}
	}
	return doctorCheck{Name: "data_plane", Status: doctorOK, Message: summary.BaseURL}
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
