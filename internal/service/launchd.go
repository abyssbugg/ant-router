package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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

// loadedJob reports whether launchd has label loaded from our own definition.
// It returns ErrLabelInUse when the label is loaded from anywhere else, or
// when our plist at plistPath is not an owned definition: only a job loaded
// from our owned plist may be booted out. launchctl print names the loaded
// definition on its "path =" line; a loaded job without one fails closed.
func (b *LaunchdBackend) loadedJob(ctx context.Context, label, plistPath string, owned bool) (bool, error) {
	stdout, stderr, err := b.runner.Run(ctx, "launchctl", "print", b.target(label))
	if err != nil {
		if !isLaunchdNotLoaded(stderr) {
			return false, commandError("query service", err, stderr)
		}
		return false, nil
	}
	if !owned || !samePath(launchdLoadedPath(stdout), plistPath) {
		return false, ErrLabelInUse{Label: label}
	}
	return true, nil
}

// launchdLoadedPath returns the definition path from launchctl print output,
// or "" if none is reported.
func launchdLoadedPath(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "path = "); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// samePath reports whether a and b name the same file, resolving symlinked
// directories such as /var -> /private/var that launchd may report resolved.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && resolvedA == resolvedB
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
	owned, err := requireReplaceable(plistPath, launchdOwnerLine)
	if err != nil {
		return err
	}
	// The label may name an agent loaded from another directory (for example
	// /Library/LaunchAgents); never replace it. Checked here without side
	// effects, and again under the lock before acting on it.
	if _, err := b.loadedJob(ctx, spec.Label, plistPath, owned); err != nil {
		return err
	}

	lock, err := lockLabel(ctx, agentsDir, spec.Label)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	// Re-check under the lock: another process may have changed the file.
	owned, err = requireReplaceable(plistPath, launchdOwnerLine)
	if err != nil {
		return err
	}
	loaded, err := b.loadedJob(ctx, spec.Label, plistPath, owned)
	if err != nil {
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

	// Reload our own job so it runs the new definition. Only a job verified
	// above as loaded from our owned plist is booted out; if a foreign job
	// takes the label meanwhile, bootstrap fails rather than replacing it.
	if loaded {
		if err := b.bootout(ctx, b.target(spec.Label)); err != nil {
			return err
		}
	}

	_, bootstrapStderr, err := b.runner.Run(ctx, "launchctl", "bootstrap", "gui/"+strconv.Itoa(b.uid), plistPath)
	if err != nil {
		bootstrapErr := commandError("bootstrap service", err, bootstrapStderr)
		if !owned {
			// A failed fresh install must not leave an owned plist claiming
			// a label that may belong to another job.
			if removeErr := os.Remove(plistPath); removeErr != nil && !os.IsNotExist(removeErr) {
				return errors.Join(bootstrapErr, fmt.Errorf("remove plist: %w", removeErr))
			}
		}
		return bootstrapErr
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

// Status returns the status of a LaunchAgent.
//
// Installation is decided by the plist on disk. An installed plist that
// launchd does not know about (exit status 113, "Could not find service")
// is reported as stopped with detail "not loaded".
func (b *LaunchdBackend) Status(ctx context.Context, label string) (Status, error) {
	if err := ValidateLabel(label); err != nil {
		return Status{}, err
	}
	plistPath := filepath.Join(b.home, "Library", "LaunchAgents", label+".plist")
	if _, err := os.Lstat(plistPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Status{State: StateNotInstalled}, nil
		}
		// An uninspectable definition is not proof of absence.
		return Status{}, fmt.Errorf("stat plist: %w", err)
	}

	stdout, stderr, err := b.runner.Run(ctx, "launchctl", "print", "gui/"+strconv.Itoa(b.uid)+"/"+label)
	if err != nil {
		var exitErr *exec.ExitError
		if (errors.As(err, &exitErr) && exitErr.ExitCode() == launchctlNotFoundExit) || isLaunchdNotLoaded(stderr) {
			return Status{State: StateStopped, Detail: "not loaded"}, nil
		}
		return Status{}, commandError("query service", err, stderr)
	}

	state, ok := launchdState(stdout)
	switch {
	case !ok:
		return Status{State: StateUnknown}, nil
	case state == "running":
		return Status{State: StateRunning}, nil
	case state == "not running" || state == "waiting" || state == "exited":
		return Status{State: StateStopped}, nil
	default:
		return Status{State: StateUnknown, Detail: state}, nil
	}
}

// launchctlNotFoundExit is launchctl's exit status for an unknown service.
const launchctlNotFoundExit = 113

// launchdState extracts the first "state = <value>" line from launchctl
// print output.
func launchdState(out []byte) (string, bool) {
	for _, line := range strings.Split(string(out), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && strings.TrimSpace(key) == "state" {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}
