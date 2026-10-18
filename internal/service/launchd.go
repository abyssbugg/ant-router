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
	buf.WriteString("  " + launchdOwnerLine + "\n")

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

// isLaunchdNotLoaded checks if stderr indicates the service is not loaded.
func isLaunchdNotLoaded(stderr []byte) bool {
	stderrStr := string(stderr)
	return strings.Contains(stderrStr, "Could not find service") || strings.Contains(stderrStr, "No such process")
}

func (b *LaunchdBackend) target(label string) string {
	return "gui/" + strconv.Itoa(b.uid) + "/" + label
}

// bootout unloads target, treating an already-unloaded job as success.
func (b *LaunchdBackend) bootout(ctx context.Context, target string) error {
	_, stderr, err := b.runner.Run(ctx, "launchctl", "bootout", target)
	if err != nil && !isLaunchdNotLoaded(stderr) {
		return commandError("bootout service", err, stderr)
	}
	return nil
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

	// Refuse to replace a definition we do not own before any side effect.
	if err := requireReplaceable(plistPath, launchdOwnerLine); err != nil {
		return err
	}

	lock, err := lockLabel(ctx, agentsDir, spec.Label)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	// Re-check under the lock: another process may have changed the file.
	if err := requireReplaceable(plistPath, launchdOwnerLine); err != nil {
		return err
	}

	// Create log directory if needed
	logDir := filepath.Dir(spec.LogPath)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Atomic write
	_, err = atomicfile.Write(ctx, plistPath, plist, 0o644)
	if err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Bootout (ignore not-loaded error only)
	if err := b.bootout(ctx, b.target(spec.Label)); err != nil {
		return err
	}

	// Bootstrap
	_, bootstrapStderr, err := b.runner.Run(ctx, "launchctl", "bootstrap", "gui/"+strconv.Itoa(b.uid), plistPath)
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
	// Uninstall never touches a definition we do not own. Without our plist
	// the label may name an agent loaded from another directory (for example
	// /Library/LaunchAgents), so a missing plist means there is nothing to do.
	err := b.withOwnedPlist(ctx, label, func(plistPath string) error {
		if err := b.bootout(ctx, b.target(label)); err != nil {
			return err
		}
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove plist: %w", err)
		}
		return nil
	})
	if isNotInstalled(err) {
		return nil
	}
	return err
}

// withOwnedPlist runs fn while holding the label lock, after verifying the
// plist is ours both before and after acquiring it.
func (b *LaunchdBackend) withOwnedPlist(ctx context.Context, label string, fn func(plistPath string) error) error {
	agentsDir := filepath.Join(b.home, "Library", "LaunchAgents")
	plistPath := filepath.Join(agentsDir, label+".plist")
	if err := readOwnedDefinition(plistPath, launchdOwnerLine); err != nil {
		return err
	}
	lock, err := lockLabel(ctx, agentsDir, label)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	if err := readOwnedDefinition(plistPath, launchdOwnerLine); err != nil {
		return err
	}
	return fn(plistPath)
}

// Start starts a service.
func (b *LaunchdBackend) Start(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	return b.withOwnedPlist(ctx, label, func(plistPath string) error {
		target := b.target(label)
		_, stderr, err := b.runner.Run(ctx, "launchctl", "print", target)
		if err != nil {
			if !isLaunchdNotLoaded(stderr) {
				return commandError("query service", err, stderr)
			}
			// Stop unloads the job; bootstrapping reloads it and RunAtLoad
			// starts it.
			_, stderr, err = b.runner.Run(ctx, "launchctl", "bootstrap", "gui/"+strconv.Itoa(b.uid), plistPath)
			if err != nil {
				return commandError("start service", err, stderr)
			}
			return nil
		}
		// Loaded: kickstart without -k starts the job if needed and leaves a
		// running job undisturbed.
		_, stderr, err = b.runner.Run(ctx, "launchctl", "kickstart", target)
		if err != nil {
			return commandError("start service", err, stderr)
		}
		return nil
	})
}

// Stop stops a service.
func (b *LaunchdBackend) Stop(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	// KeepAlive{SuccessfulExit=false} restarts a signalled job, so stopping
	// means unloading it. An already-unloaded job is already stopped.
	return b.withOwnedPlist(ctx, label, func(string) error {
		return b.bootout(ctx, b.target(label))
	})
}

// Status returns the status of a service.
func (b *LaunchdBackend) Status(ctx context.Context, label string) (Status, error) {
	if err := ValidateLabel(label); err != nil {
		return Status{}, err
	}
	stdout, stderr, err := b.runner.Run(ctx, "launchctl", "print", b.target(label))

	if err != nil {
		if !isLaunchdNotLoaded(stderr) {
			return Status{}, commandError("query service", err, stderr)
		}
		plistPath := filepath.Join(b.home, "Library", "LaunchAgents", label+".plist")
		if _, statErr := os.Stat(plistPath); statErr == nil {
			return Status{State: StateStopped}, nil
		}
		return Status{State: StateNotInstalled}, nil
	}

	outputStr := string(stdout)
	if strings.Contains(outputStr, "state = running") {
		return Status{State: StateRunning}, nil
	}

	return Status{State: StateStopped}, nil
}
