package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/filelock"
)

// OwnershipMarker is the marker constant for service ownership.
const OwnershipMarker = "github.com/abyssbugg/ant-router"

// State represents the lifecycle state of a service.
type State int

const (
	StateUnknown State = iota
	StateNotInstalled
	StateStopped
	StateRunning
)

func (s State) String() string {
	switch s {
	case StateUnknown:
		return "unknown"
	case StateNotInstalled:
		return "not installed"
	case StateStopped:
		return "stopped"
	case StateRunning:
		return "running"
	default:
		return "unknown"
	}
}

// ErrRelativePath is returned when a path is relative instead of absolute.
type ErrRelativePath struct {
	Path string
}

func (e ErrRelativePath) Error() string {
	return fmt.Sprintf("path must be absolute: %s", e.Path)
}

// ErrNotRegularFile is returned when a file is not a regular file.
type ErrNotRegularFile struct {
	Path   string
	Reason string
}

func (e ErrNotRegularFile) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s is not a regular file: %s", e.Path, e.Reason)
	}
	return fmt.Sprintf("%s is not a regular file", e.Path)
}

// ErrNotOwned is returned when a file is not owned by the service.
type ErrNotOwned struct {
	Path string
}

func (e ErrNotOwned) Error() string {
	return fmt.Sprintf("file not owned by service: %s", e.Path)
}

// ErrNotInstalled is returned when a service definition file does not exist.
type ErrNotInstalled struct {
	Path string
}

func (e ErrNotInstalled) Error() string {
	return fmt.Sprintf("service definition %s is not installed", e.Path)
}

// ErrLabelInUse is returned when Install finds the label already loaded by
// the service manager without a definition this package owns, for example an
// agent loaded from another directory. Install never replaces such a job.
type ErrLabelInUse struct {
	Label string
}

func (e ErrLabelInUse) Error() string {
	return fmt.Sprintf("service %s is loaded from a definition this tool does not own", e.Label)
}

// ErrInvalidLabel is returned when a label is invalid.
type ErrInvalidLabel struct {
	Label  string
	Reason string
}

func (e ErrInvalidLabel) Error() string {
	return fmt.Sprintf("invalid label: %s", e.Reason)
}

// hasControlCharacters checks for control characters (U+0000-U+001F, U+007F) or invalid UTF-8.
func hasControlCharacters(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, ch := range s {
		if (ch >= 0 && ch <= 0x1F) || ch == 0x7F {
			return true
		}
	}
	return false
}

// ValidateLabel checks if a label is valid.
// Valid labels are 1-128 ASCII characters (letters, digits, '.', '-', '_'),
// must start with a letter or digit, and contain no '..' sequence.
func ValidateLabel(label string) error {
	if label == "" {
		return ErrInvalidLabel{Label: label, Reason: "empty label"}
	}
	if len(label) > 128 {
		return ErrInvalidLabel{Label: label, Reason: "label too long (max 128 chars)"}
	}
	if !utf8.ValidString(label) {
		return ErrInvalidLabel{Label: label, Reason: "invalid UTF-8"}
	}

	first := rune(label[0])
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9')) {
		return ErrInvalidLabel{Label: label, Reason: "must start with letter or digit"}
	}

	if len(label) > 1 && label[0] == '.' {
		return ErrInvalidLabel{Label: label, Reason: "cannot start with dot"}
	}

	for i, ch := range label {
		if ch > 127 {
			return ErrInvalidLabel{Label: label, Reason: fmt.Sprintf("invalid character: %q", ch)}
		}
		isValid := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_'
		if !isValid {
			return ErrInvalidLabel{Label: label, Reason: fmt.Sprintf("invalid character: %q", ch)}
		}
		if ch == '.' && i+1 < len(label) && label[i+1] == '.' {
			return ErrInvalidLabel{Label: label, Reason: "contains '..' sequence"}
		}
	}

	return nil
}

// Ownership is recorded as a dedicated line in each definition format so it
// cannot be forged by data that merely mentions the marker. In a plist, '<'
// inside a string value is escaped, so a raw comment line is structural; in a
// unit file, X- prefixed keys are ignored by systemd.
const (
	launchdOwnerLine = "<!-- owned by " + OwnershipMarker + " -->"
	systemdOwnerLine = "X-AntRouter-Owner=" + OwnershipMarker
)

// hasOwnerLine reports whether data contains ownerLine as a complete line
// (surrounding whitespace ignored) that does not continue a previous line
// ending in a backslash.
func hasOwnerLine(data []byte, ownerLine string) bool {
	continued := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !continued && line == ownerLine {
			return true
		}
		continued = strings.HasSuffix(line, `\`)
	}
	return false
}

// readOwnedDefinition checks if a service definition file exists and is owned.
// Returns nil if file exists and contains OwnershipMarker.
// Returns ErrNotInstalled if file does not exist.
// Returns ErrNotRegularFile if the path is not a regular file (a FIFO,
// device, or directory), checked on the opened descriptor so the type cannot
// change between the check and the read.
// Returns ErrNotOwned if file exists but does not contain OwnershipMarker.
// Returns wrapped error for other read errors.
func readOwnedDefinition(path, ownerLine string) error {
	file, err := openDefinition(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotInstalled{Path: path}
		}
		return fmt.Errorf("read definition: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("read definition: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ErrNotRegularFile{Path: path, Reason: "service definition must be a regular file"}
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read definition: %w", err)
	}
	if !hasOwnerLine(data, ownerLine) {
		return ErrNotOwned{Path: path}
	}
	return nil
}

// ErrUnsupportedSystemd reports a systemd too old for the append: output
// directives the rendered unit relies on.
type ErrUnsupportedSystemd struct {
	Version int
}

func (e ErrUnsupportedSystemd) Error() string {
	return fmt.Sprintf("systemd %d is not supported: StandardOutput=append: requires systemd 240 or newer", e.Version)
}

// requireReplaceable permits a missing definition and refuses one that
// exists without the ownership marker. owned reports whether an owned
// definition exists, so callers act on a loaded job only when it is ours.
func requireReplaceable(path, ownerLine string) (owned bool, err error) {
	err = readOwnedDefinition(path, ownerLine)
	if err == nil {
		return true, nil
	}
	var missing ErrNotInstalled
	if errors.As(err, &missing) {
		return false, nil
	}
	return false, err
}

// isNotInstalled reports whether err says the definition file is missing.
func isNotInstalled(err error) bool {
	var missing ErrNotInstalled
	return errors.As(err, &missing)
}

// lockLabel serializes lifecycle operations for one label across processes,
// so an Install and an Uninstall can never interleave their steps. The lock
// file sits beside the definitions; launchd and systemd only load files with
// their own definition suffixes.
func lockLabel(ctx context.Context, dir, label string) (*filelock.Lock, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	lock, err := filelock.Acquire(ctx, filepath.Join(filepath.Clean(dir), "."+label+".lock"))
	if err != nil {
		return nil, fmt.Errorf("lock service %s: %w", label, err)
	}
	return lock, nil
}

// Spec defines a service specification.
type Spec struct {
	Label      string   // service label
	Executable string   // absolute path to executable
	Args       []string // command arguments
	LogPath    string   // absolute path to log file
}

// Validate checks the spec for invalid values.
func (s *Spec) Validate() error {
	if err := ValidateLabel(s.Label); err != nil {
		return err
	}
	if hasControlCharacters(s.Executable) {
		return fmt.Errorf("executable contains control characters or invalid UTF-8")
	}
	if !filepath.IsAbs(s.Executable) {
		return ErrRelativePath{Path: s.Executable}
	}
	info, err := os.Stat(s.Executable)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotRegularFile{Path: s.Executable, Reason: "file does not exist"}
		}
		return ErrNotRegularFile{Path: s.Executable, Reason: err.Error()}
	}
	if !info.Mode().IsRegular() {
		return ErrNotRegularFile{Path: s.Executable, Reason: "not a regular file"}
	}
	// Check execute permission for owner (user can execute)
	// On Windows, Go does not report Unix execute bits, so skip this check
	if runtime.GOOS != "windows" && info.Mode()&0o100 == 0 {
		return ErrNotRegularFile{Path: s.Executable, Reason: "missing execute permission"}
	}
	for _, arg := range s.Args {
		if hasControlCharacters(arg) {
			return fmt.Errorf("argument contains control characters or invalid UTF-8")
		}
	}
	if hasControlCharacters(s.LogPath) {
		return fmt.Errorf("log path contains control characters or invalid UTF-8")
	}
	if !filepath.IsAbs(s.LogPath) {
		return ErrRelativePath{Path: s.LogPath}
	}
	return nil
}

// Status represents the current status of a service.
type Status struct {
	State  State
	Detail string
}

// Runner is the interface for executing commands.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// Manager is the interface for managing services.
type Manager interface {
	Install(ctx context.Context, spec Spec) error
	Uninstall(ctx context.Context, label string) error
	Start(ctx context.Context, label string) error
	Stop(ctx context.Context, label string) error
	Status(ctx context.Context, label string) (Status, error)
}

// ErrUnsupportedLogPath is returned when a log path cannot be represented
// safely by the selected service backend.
type ErrUnsupportedLogPath struct {
	Path   string
	Reason string
}

func (e ErrUnsupportedLogPath) Error() string {
	return fmt.Sprintf("unsupported log path %q: %s", e.Path, e.Reason)
}
