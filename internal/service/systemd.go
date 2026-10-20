package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
)

// renderSystemdUnit renders a systemd service unit file from a Spec.
// It validates the spec, checks that LogPath is suitable for systemd,
// and returns the unit file content as bytes.
func renderSystemdUnit(spec Spec) ([]byte, error) {
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid spec: %w", err)
	}

	// Validate LogPath for systemd: no whitespace, %, or $
	if strings.ContainsAny(spec.LogPath, " \t\n\r") {
		return nil, ErrUnsupportedLogPath{Path: spec.LogPath, Reason: "contains whitespace, which systemd cannot express in StandardOutput/StandardError"}
	}
	if strings.ContainsAny(spec.LogPath, "%") {
		return nil, ErrUnsupportedLogPath{Path: spec.LogPath, Reason: "contains percent, which systemd cannot express in StandardOutput/StandardError"}
	}
	if strings.ContainsAny(spec.LogPath, "$") {
		return nil, ErrUnsupportedLogPath{Path: spec.LogPath, Reason: "contains dollar, which systemd cannot express in StandardOutput/StandardError"}
	}
	// The path is unquoted: systemd reads backslash as an escape and a
	// trailing backslash as a line continuation.
	if strings.Contains(spec.LogPath, `\`) {
		return nil, ErrUnsupportedLogPath{Path: spec.LogPath, Reason: "contains backslash, which systemd cannot express in StandardOutput/StandardError"}
	}

	var buf bytes.Buffer

	buf.WriteString("[Unit]\n")
	buf.WriteString(fmt.Sprintf("Description=Ant Router daemon (managed by %s)\n", OwnershipMarker))
	buf.WriteString(systemdOwnerLine + "\n")
	buf.WriteString("After=network-online.target\n")
	buf.WriteString("\n")

	buf.WriteString("[Service]\n")
	buf.WriteString("Type=simple\n")

	// ExecStart with proper quoting
	execStart := quoteSystemdExecStart(spec.Executable, spec.Args)
	buf.WriteString(fmt.Sprintf("ExecStart=%s\n", execStart))

	buf.WriteString("Restart=on-failure\n")
	buf.WriteString("RestartSec=2\n")
	buf.WriteString(fmt.Sprintf("StandardOutput=append:%s\n", spec.LogPath))
	buf.WriteString(fmt.Sprintf("StandardError=append:%s\n", spec.LogPath))
	buf.WriteString("\n")

	buf.WriteString("[Install]\n")
	buf.WriteString("WantedBy=default.target\n")

	return buf.Bytes(), nil
}

// quoteSystemdExecStart builds an ExecStart value with proper systemd quoting.
// Every argument (exe and args) is wrapped in double quotes, with:
// - backslash escaped as \\
// - double quote escaped as \"
// - percent escaped as %%
// - dollar escaped as $$
func quoteSystemdExecStart(exe string, args []string) string {
	var buf bytes.Buffer

	// Quote executable
	buf.WriteString(quoteSystemdWord(exe))

	// Quote each argument
	for _, arg := range args {
		buf.WriteString(" ")
		buf.WriteString(quoteSystemdWord(arg))
	}

	return buf.String()
}

// quoteSystemdWord quotes a single word for systemd ExecStart.
// Wraps the word in double quotes and escapes special characters.
func quoteSystemdWord(s string) string {
	var buf bytes.Buffer
	buf.WriteString("\"")

	for _, ch := range s {
		switch ch {
		case '\\':
			// systemd C-unescapes quoted words, so one literal backslash is
			// written as two.
			buf.WriteString("\\\\")
		case '"':
			buf.WriteString("\\\"")
		case '%':
			buf.WriteString("%%")
		case '$':
			buf.WriteString("$$")
		default:
			buf.WriteRune(ch)
		}
	}

	buf.WriteString("\"")
	return buf.String()
}

// SystemdBackend manages service lifecycle via systemd --user.
type SystemdBackend struct {
	configDir string
	runner    Runner
}

// Compile-time assertion that *SystemdBackend implements Manager.
var _ Manager = (*SystemdBackend)(nil)

// NewSystemdBackend creates a systemd backend.
// configDir is the XDG config home; units will be stored at configDir/systemd/user.
func NewSystemdBackend(configDir string, runner Runner) *SystemdBackend {
	return &SystemdBackend{
		configDir: configDir,
		runner:    runner,
	}
}

// Install installs a service.
// requireAppendSupport probes the systemd version and rejects versions that
// predate StandardOutput=append: (systemd 240).
func (b *SystemdBackend) requireAppendSupport(ctx context.Context) error {
	stdout, stderr, err := b.runner.Run(ctx, "systemctl", "--version")
	if err != nil {
		return commandError("query systemd version", err, stderr)
	}
	line, _, _ := strings.Cut(string(stdout), "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "systemd" {
		return fmt.Errorf("query systemd version: unrecognised output %q", line)
	}
	version, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("query systemd version: unrecognised version %q", fields[1])
	}
	if version < 240 {
		return ErrUnsupportedSystemd{Version: version}
	}
	return nil
}

func (b *SystemdBackend) Install(ctx context.Context, spec Spec) error {
	if err := ValidateLabel(spec.Label); err != nil {
		return err
	}

	if err := spec.Validate(); err != nil {
		return fmt.Errorf("invalid spec: %w", err)
	}

	unitName := spec.Label + ".service"
	unitPath := filepath.Join(b.configDir, "systemd", "user", unitName)

	// Render unit
	unit, err := renderSystemdUnit(spec)
	if err != nil {
		return err
	}

	// Refuse to replace a unit we do not own before any side effect.
	if _, err := requireReplaceable(unitPath, systemdOwnerLine); err != nil {
		return err
	}

	// The unit's append: output directives need systemd 240+.
	if err := b.requireAppendSupport(ctx); err != nil {
		return err
	}

	lock, err := lockLabel(ctx, filepath.Dir(unitPath), spec.Label)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	// Re-check under the lock: another process may have changed the file.
	if _, err := requireReplaceable(unitPath, systemdOwnerLine); err != nil {
		return err
	}

	// Create log directory if needed
	logDir := filepath.Dir(spec.LogPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Write unit atomically
	_, err = atomicfile.Write(ctx, unitPath, unit, 0644)
	if err != nil {
		return fmt.Errorf("write unit: %w", err)
	}

	// Run daemon-reload
	_, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
	if err != nil {
		return commandError("daemon-reload", err, stderr)
	}

	// Enable and (re)start as separate steps so a failure is attributed to
	// the step that failed. restart, not start, so a reinstall runs the new
	// definition. If either step fails, undo only an enablement this Install
	// created: a unit that was already enabled stays as the user had it.
	wasEnabled, err := b.isEnabled(ctx, unitName)
	if err != nil {
		return err
	}
	if _, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "enable", unitName); err != nil {
		return b.undoEnable(ctx, unitName, wasEnabled, commandError("enable", err, stderr))
	}
	if _, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "restart", unitName); err != nil {
		return b.undoEnable(ctx, unitName, wasEnabled, commandError("restart", err, stderr))
	}
	return nil
}

// rollbackTimeout bounds cleanup that must run even after the caller's
// context is cancelled.
const rollbackTimeout = 10 * time.Second

// isEnabled reports whether unitName is enabled. systemctl is-enabled exits
// 0 for enabled states and non-zero with the state on stdout otherwise; a
// failure with no state is a query error, not "disabled".
func (b *SystemdBackend) isEnabled(ctx context.Context, unitName string) (bool, error) {
	stdout, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "is-enabled", unitName)
	if err == nil {
		return true, nil
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return false, nil
	}
	return false, commandError("query enablement", err, stderr)
}

// undoEnable disables unitName after a failed Install unless it was enabled
// beforehand. The rollback ignores the caller's cancellation, which is
// often what made the install fail, but is still bounded.
func (b *SystemdBackend) undoEnable(ctx context.Context, unitName string, wasEnabled bool, cause error) error {
	if wasEnabled {
		return cause
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	if _, stderr, err := b.runner.Run(cleanupCtx, "systemctl", "--user", "disable", unitName); err != nil {
		return errors.Join(cause, commandError("undo enable", err, stderr))
	}
	return cause
}

// Uninstall uninstalls a service.
func (b *SystemdBackend) Uninstall(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}

	unitName := label + ".service"
	unitPath := filepath.Join(b.configDir, "systemd", "user", unitName)

	// Uninstall never touches a unit we do not own. Without our unit file
	// the label may name a unit loaded from another directory, so only the
	// label-free daemon-reload runs; it also completes an earlier uninstall
	// whose reload failed after the file was removed.
	if done, err := b.uninstallPrecheck(ctx, unitPath); done || err != nil {
		return err
	}
	lock, err := lockLabel(ctx, filepath.Dir(unitPath), label)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	if done, err := b.uninstallPrecheck(ctx, unitPath); done || err != nil {
		return err
	}

	// Disable and stop (ignore "not loaded" / "does not exist" errors)
	_, disableStderr, disableErr := b.runner.Run(ctx, "systemctl", "--user", "disable", "--now", unitName)

	if disableErr != nil {
		stderrStr := string(disableStderr)
		// Ignore only "not loaded", "does not exist", or "No such file" errors
		if !strings.Contains(stderrStr, "not loaded") &&
			!strings.Contains(stderrStr, "does not exist") &&
			!strings.Contains(stderrStr, "No such file") {
			return commandError("disable", disableErr, disableStderr)
		}
	}

	// Remove file
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit: %w", err)
	}

	// Run daemon-reload
	_, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
	if err != nil {
		return commandError("daemon-reload", err, stderr)
	}

	return nil
}

// uninstallPrecheck returns done=true when there is nothing of ours to
// uninstall (after reloading), or an error when the unit is not ours.
func (b *SystemdBackend) uninstallPrecheck(ctx context.Context, unitPath string) (bool, error) {
	err := readOwnedDefinition(unitPath, systemdOwnerLine)
	if isNotInstalled(err) {
		if _, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return true, commandError("daemon-reload", err, stderr)
		}
		return true, nil
	}
	return false, err
}

// withOwnedUnit runs fn while holding the label lock, after verifying the
// unit file is ours both before and after acquiring it.
func (b *SystemdBackend) withOwnedUnit(ctx context.Context, label string, fn func(unitName string) error) error {
	unitName := label + ".service"
	unitPath := filepath.Join(b.configDir, "systemd", "user", unitName)
	if err := readOwnedDefinition(unitPath, systemdOwnerLine); err != nil {
		return err
	}
	lock, err := lockLabel(ctx, filepath.Dir(unitPath), label)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	if err := readOwnedDefinition(unitPath, systemdOwnerLine); err != nil {
		return err
	}
	return fn(unitName)
}

// Start starts a service.
func (b *SystemdBackend) Start(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	return b.withOwnedUnit(ctx, label, func(unitName string) error {
		_, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "start", unitName)
		if err != nil {
			return commandError("start", err, stderr)
		}
		return nil
	})
}

// Stop stops a service.
func (b *SystemdBackend) Stop(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	return b.withOwnedUnit(ctx, label, func(unitName string) error {
		_, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "stop", unitName)
		if err != nil {
			return commandError("stop", err, stderr)
		}
		return nil
	})
}

// Status returns the status of a service.
//
// Installation is decided by the presence of the unit file rather than by
// systemctl output: `systemctl --user is-active` reports unknown units as
// "inactive" (exit status 3, empty stderr), which is indistinguishable from
// an installed but stopped unit.
func (b *SystemdBackend) Status(ctx context.Context, label string) (Status, error) {
	if err := ValidateLabel(label); err != nil {
		return Status{}, err
	}

	unitName := label + ".service"
	unitPath := filepath.Join(b.configDir, "systemd", "user", unitName)
	if _, err := os.Lstat(unitPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Status{State: StateNotInstalled}, nil
		}
		return Status{}, fmt.Errorf("stat unit: %w", err)
	}

	// is-active exits non-zero for every state other than "active", so the
	// state word on stdout is authoritative and the exit status is only used
	// when stdout carries no recognizable state.
	stdout, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "is-active", unitName)
	switch strings.TrimSpace(string(stdout)) {
	case "active":
		return Status{State: StateRunning}, nil
	case "inactive":
		return Status{State: StateStopped}, nil
	case "failed":
		return Status{State: StateStopped, Detail: "failed"}, nil
	case "activating", "deactivating", "reloading", "refreshing":
		return Status{State: StateUnknown, Detail: strings.TrimSpace(string(stdout))}, nil
	}
	if err != nil {
		return Status{}, commandError("is-active", err, stderr)
	}
	return Status{State: StateUnknown}, nil
}
