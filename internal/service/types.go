package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
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
	if info.Mode()&0o100 == 0 {
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
