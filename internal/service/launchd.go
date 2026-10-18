package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
)

// renderLaunchdPlist generates an XML plist for a service spec.
func renderLaunchdPlist(spec Spec) ([]byte, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString("\n")
	buf.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
	buf.WriteString("\n")
	buf.WriteString(`<plist version="1.0">`)
	buf.WriteString("\n")
	buf.WriteString(`<dict>`)
	buf.WriteString("\n")

	// ownership marker
	buf.WriteString(`  <!-- owned by `)
	buf.WriteString(escapeXMLString(OwnershipMarker))
	buf.WriteString(` -->`)
	buf.WriteString("\n")

	// Label
	buf.WriteString(`  <key>Label</key>`)
	buf.WriteString("\n")
	buf.WriteString(`  <string>`)
	buf.WriteString(escapeXMLString(spec.Label))
	buf.WriteString(`</string>`)
	buf.WriteString("\n")

	// ProgramArguments
	buf.WriteString(`  <key>ProgramArguments</key>`)
	buf.WriteString("\n")
	buf.WriteString(`  <array>`)
	buf.WriteString("\n")
	buf.WriteString(`    <string>`)
	buf.WriteString(escapeXMLString(spec.Executable))
	buf.WriteString(`</string>`)
	buf.WriteString("\n")
	for _, arg := range spec.Args {
		buf.WriteString(`    <string>`)
		buf.WriteString(escapeXMLString(arg))
		buf.WriteString(`</string>`)
		buf.WriteString("\n")
	}
	buf.WriteString(`  </array>`)
	buf.WriteString("\n")

	// RunAtLoad
	buf.WriteString(`  <key>RunAtLoad</key>`)
	buf.WriteString("\n")
	buf.WriteString(`  <true/>`)
	buf.WriteString("\n")

	// KeepAlive
	buf.WriteString(`  <key>KeepAlive</key>`)
	buf.WriteString("\n")
	buf.WriteString(`  <dict>`)
	buf.WriteString("\n")
	buf.WriteString(`    <key>SuccessfulExit</key>`)
	buf.WriteString("\n")
	buf.WriteString(`    <false/>`)
	buf.WriteString("\n")
	buf.WriteString(`  </dict>`)
	buf.WriteString("\n")

	// StandardOutPath
	buf.WriteString(`  <key>StandardOutPath</key>`)
	buf.WriteString("\n")
	buf.WriteString(`  <string>`)
	buf.WriteString(escapeXMLString(spec.LogPath))
	buf.WriteString(`</string>`)
	buf.WriteString("\n")

	// StandardErrorPath
	buf.WriteString(`  <key>StandardErrorPath</key>`)
	buf.WriteString("\n")
	buf.WriteString(`  <string>`)
	buf.WriteString(escapeXMLString(spec.LogPath))
	buf.WriteString(`</string>`)
	buf.WriteString("\n")

	// ProcessType
	buf.WriteString(`  <key>ProcessType</key>`)
	buf.WriteString("\n")
	buf.WriteString(`  <string>Background</string>`)
	buf.WriteString("\n")

	buf.WriteString(`</dict>`)
	buf.WriteString("\n")
	buf.WriteString(`</plist>`)
	buf.WriteString("\n")

	return buf.Bytes(), nil
}

// escapeXMLString escapes s for XML character data. Spec.Validate rejects
// control characters and invalid UTF-8 beforehand, so no input is altered.
func escapeXMLString(s string) string {
	var escaped strings.Builder
	// xml.EscapeText only fails when the writer fails; strings.Builder does not.
	_ = xml.EscapeText(&escaped, []byte(s))
	return escaped.String()
}

// commandError wraps err with at most maxStderrBytes of trimmed stderr,
// cut on a UTF-8 rune boundary.
func commandError(op string, err error, stderr []byte) error {
	const maxStderrBytes = 256
	trimmed := stderr
	if len(trimmed) > maxStderrBytes {
		trimmed = trimmed[:maxStderrBytes]
		for back := 0; back < utf8.UTFMax && len(trimmed) > 0 && !utf8.Valid(trimmed); back++ {
			trimmed = trimmed[:len(trimmed)-1]
		}
	}
	detail := strings.TrimSpace(string(trimmed))
	if detail == "" {
		return fmt.Errorf("%s: %w", op, err)
	}
	return fmt.Errorf("%s: %w (stderr: %s)", op, err, detail)
}

// LaunchdBackend manages service lifecycle via launchctl.
type LaunchdBackend struct {
	home   string
	uid    int
	runner Runner
}

// Compile-time assertion that *LaunchdBackend implements Manager.
var _ Manager = (*LaunchdBackend)(nil)

// NewLaunchdBackend creates a launchd backend.
func NewLaunchdBackend(home string, uid int, runner Runner) *LaunchdBackend {
	return &LaunchdBackend{
		home:   home,
		uid:    uid,
		runner: runner,
	}
}

// Install installs a service.
func (b *LaunchdBackend) Install(ctx context.Context, spec Spec) error {
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("invalid spec: %w", err)
	}

	plist, err := renderLaunchdPlist(spec)
	if err != nil {
		return fmt.Errorf("render plist: %w", err)
	}

	agentsDir := filepath.Join(b.home, "Library", "LaunchAgents")
	plistPath := filepath.Join(agentsDir, spec.Label+".plist")

	// Create directory
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	// Create log directory if needed
	logDir := filepath.Dir(spec.LogPath)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Check if file exists and is owned
	if existing, err := os.ReadFile(plistPath); err == nil {
		if !bytes.Contains(existing, []byte(OwnershipMarker)) {
			return ErrNotOwned{Path: plistPath}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read plist: %w", err)
	}

	// Atomic write
	_, err = atomicfile.Write(ctx, plistPath, plist, 0o644)
	if err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Bootout (ignore not-loaded error only)
	guid := "gui/" + strconv.Itoa(b.uid)
	label := guid + "/" + spec.Label
	bootoutStdout, bootoutStderr, bootoutErr := b.runner.Run(ctx, "launchctl", "bootout", label)
	_ = bootoutStdout
	if bootoutErr != nil {
		stderrStr := string(bootoutStderr)
		// Only ignore "not loaded" errors; other errors indicate a real problem
		if !strings.Contains(stderrStr, "Could not find service") && !strings.Contains(stderrStr, "No such process") {
			return commandError("bootout service", bootoutErr, bootoutStderr)
		}
	}

	// Bootstrap
	_, bootstrapStderr, err := b.runner.Run(ctx, "launchctl", "bootstrap", guid, plistPath)
	if err != nil {
		return commandError("bootstrap service", err, bootstrapStderr)
	}

	return nil
}

// Uninstall removes a service.
func (b *LaunchdBackend) Uninstall(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	agentsDir := filepath.Join(b.home, "Library", "LaunchAgents")
	plistPath := filepath.Join(agentsDir, label+".plist")

	// Check if file exists and is owned
	data, err := os.ReadFile(plistPath)
	if err == nil {
		if !bytes.Contains(data, []byte(OwnershipMarker)) {
			return ErrNotOwned{Path: plistPath}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read plist: %w", err)
	}

	// Bootout (ignore "not loaded" error only)
	guid := "gui/" + strconv.Itoa(b.uid)
	bootoutLabel := guid + "/" + label
	_, bootoutStderr, bootoutErr := b.runner.Run(ctx, "launchctl", "bootout", bootoutLabel)

	if bootoutErr != nil {
		stderrStr := string(bootoutStderr)
		if !strings.Contains(stderrStr, "Could not find service") && !strings.Contains(stderrStr, "No such process") {
			return commandError("bootout service", bootoutErr, bootoutStderr)
		}
	}

	// Remove file
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	return nil
}

// Start starts a service.
func (b *LaunchdBackend) Start(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	agentsDir := filepath.Join(b.home, "Library", "LaunchAgents")
	plistPath := filepath.Join(agentsDir, label+".plist")

	// Check if file exists and is owned
	if err := readOwnedDefinition(plistPath); err != nil {
		return err
	}

	guid := "gui/" + strconv.Itoa(b.uid)
	kickstartLabel := guid + "/" + label
	_, stderr, err := b.runner.Run(ctx, "launchctl", "kickstart", "-k", kickstartLabel)
	if err != nil {
		return commandError("start service", err, stderr)
	}
	return nil
}

// Stop stops a service.
func (b *LaunchdBackend) Stop(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	agentsDir := filepath.Join(b.home, "Library", "LaunchAgents")
	plistPath := filepath.Join(agentsDir, label+".plist")

	// Check if file exists and is owned
	if err := readOwnedDefinition(plistPath); err != nil {
		return err
	}

	guid := "gui/" + strconv.Itoa(b.uid)
	killLabel := guid + "/" + label
	_, stderr, err := b.runner.Run(ctx, "launchctl", "kill", "SIGTERM", killLabel)
	if err != nil {
		return commandError("stop service", err, stderr)
	}
	return nil
}

// Status returns the status of a service.
func (b *LaunchdBackend) Status(ctx context.Context, label string) (Status, error) {
	if err := ValidateLabel(label); err != nil {
		return Status{}, err
	}
	guid := "gui/" + strconv.Itoa(b.uid)
	printLabel := guid + "/" + label
	stdout, stderr, err := b.runner.Run(ctx, "launchctl", "print", printLabel)

	if err != nil {
		stderrStr := string(stderr)
		if strings.Contains(stderrStr, "Could not find service") {
			return Status{State: StateNotInstalled}, nil
		}
		return Status{}, commandError("query service", err, stderr)
	}

	outputStr := string(stdout)
	if strings.Contains(outputStr, "state = running") {
		return Status{State: StateRunning}, nil
	}

	return Status{State: StateStopped}, nil
}
