package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

	var buf bytes.Buffer

	buf.WriteString("[Unit]\n")
	buf.WriteString(fmt.Sprintf("Description=Ant Router daemon (managed by %s)\n", OwnershipMarker))
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

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Create log directory if needed
	logDir := filepath.Dir(spec.LogPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Check if file exists and is owned
	if existing, err := os.ReadFile(unitPath); err == nil {
		if !bytes.Contains(existing, []byte(OwnershipMarker)) {
			return ErrNotOwned{Path: unitPath}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read unit: %w", err)
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

	// Enable and start
	_, stderr, err = b.runner.Run(ctx, "systemctl", "--user", "enable", "--now", unitName)
	if err != nil {
		return commandError("enable", err, stderr)
	}

	return nil
}

// Uninstall uninstalls a service.
func (b *SystemdBackend) Uninstall(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}

	unitName := label + ".service"
	unitPath := filepath.Join(b.configDir, "systemd", "user", unitName)

	// Check if file exists and is owned
	data, err := os.ReadFile(unitPath)
	switch {
	case err == nil:
		if !bytes.Contains(data, []byte(OwnershipMarker)) {
			return ErrNotOwned{Path: unitPath}
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("read unit: %w", err)
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

// Start starts a service.
func (b *SystemdBackend) Start(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}

	unitName := label + ".service"
	unitPath := filepath.Join(b.configDir, "systemd", "user", unitName)

	// Check if file exists and is owned
	data, err := os.ReadFile(unitPath)
	if err == nil {
		if !bytes.Contains(data, []byte(OwnershipMarker)) {
			return ErrNotOwned{Path: unitPath}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read unit: %w", err)
	} else {
		return ErrNotOwned{Path: unitPath}
	}

	_, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "start", unitName)
	if err != nil {
		return commandError("start", err, stderr)
	}

	return nil
}

// Stop stops a service.
func (b *SystemdBackend) Stop(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}

	unitName := label + ".service"
	unitPath := filepath.Join(b.configDir, "systemd", "user", unitName)

	// Check if file exists and is owned
	data, err := os.ReadFile(unitPath)
	if err == nil {
		if !bytes.Contains(data, []byte(OwnershipMarker)) {
			return ErrNotOwned{Path: unitPath}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read unit: %w", err)
	} else {
		return ErrNotOwned{Path: unitPath}
	}

	_, stderr, err := b.runner.Run(ctx, "systemctl", "--user", "stop", unitName)
	if err != nil {
		return commandError("stop", err, stderr)
	}

	return nil
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
