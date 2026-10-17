package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
)

// quoteWindowsArg quotes one argument so that CommandLineToArgvW (and the
// MSVC runtime) parse it back to exactly s.
//
// Arguments that are non-empty and contain no space, tab or double quote are
// returned unchanged (backslashes are literal when not followed by a quote).
// Otherwise the argument is wrapped in double quotes; every run of n
// backslashes followed by a double quote becomes 2n+1 backslashes plus the
// quote, and a run of n backslashes at the end becomes 2n backslashes so the
// closing quote is not escaped.
func quoteWindowsArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var buf strings.Builder
	buf.WriteByte('"')
	backslashes := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			backslashes++
		case '"':
			buf.WriteString(strings.Repeat("\\", 2*backslashes+1))
			buf.WriteByte('"')
			backslashes = 0
		default:
			buf.WriteString(strings.Repeat("\\", backslashes))
			buf.WriteByte(c)
			backslashes = 0
		}
	}
	buf.WriteString(strings.Repeat("\\", 2*backslashes))
	buf.WriteByte('"')
	return buf.String()
}

// taskXMLOwned reports whether a stored task XML carries the ownership
// marker. Files written by renderTaskXML are UTF-16LE with a BOM, so the
// content is decoded before searching; anything else is searched as-is.
func taskXMLOwned(data []byte) bool {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		body := data[2:]
		if len(body)%2 != 0 {
			return false
		}
		units := make([]uint16, len(body)/2)
		for i := range units {
			units[i] = uint16(body[2*i]) | uint16(body[2*i+1])<<8
		}
		return strings.Contains(string(utf16.Decode(units)), OwnershipMarker)
	}
	return bytes.Contains(data, []byte(OwnershipMarker))
}

// renderTaskXML renders a Task Scheduler 1.2 XML from a Spec.
// The XML is encoded as UTF-16LE with BOM.
func renderTaskXML(spec Spec) ([]byte, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	// Build the XML structure
	var buf bytes.Buffer

	// XML declaration with UTF-16 encoding
	buf.WriteString(`<?xml version="1.0" encoding="UTF-16"?>`)
	buf.WriteString("\n")

	// Root element with namespace
	buf.WriteString(`<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">`)
	buf.WriteString("\n")

	// RegistrationInfo with ownership marker in Description
	buf.WriteString("  <RegistrationInfo>\n")
	buf.WriteString("    <Description>")
	buf.WriteString(escapeXMLString(OwnershipMarker))
	buf.WriteString("</Description>\n")
	buf.WriteString("  </RegistrationInfo>\n")

	// Triggers with LogonTrigger for current user
	buf.WriteString("  <Triggers>\n")
	buf.WriteString("    <LogonTrigger>\n")
	buf.WriteString("      <Enabled>true</Enabled>\n")
	buf.WriteString("    </LogonTrigger>\n")
	buf.WriteString("  </Triggers>\n")

	// Principal
	buf.WriteString("  <Principal id=\"Author\">\n")
	buf.WriteString("    <LogonType>InteractiveToken</LogonType>\n")
	buf.WriteString("    <RunLevel>LeastPrivilege</RunLevel>\n")
	buf.WriteString("  </Principal>\n")

	// Settings
	buf.WriteString("  <Settings>\n")
	buf.WriteString("    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>\n")
	buf.WriteString("    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>\n")
	buf.WriteString("    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>\n")
	buf.WriteString("    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>\n")
	buf.WriteString("    <RestartOnFailure>\n")
	buf.WriteString("      <Interval>PT1M</Interval>\n")
	buf.WriteString("      <Count>3</Count>\n")
	buf.WriteString("    </RestartOnFailure>\n")
	buf.WriteString("    <Hidden>false</Hidden>\n")
	buf.WriteString("  </Settings>\n")

	// Actions with Exec
	buf.WriteString("  <Actions Context=\"Author\">\n")
	buf.WriteString("    <Exec>\n")
	buf.WriteString("      <Command>")
	buf.WriteString(escapeXMLString(spec.Executable))
	buf.WriteString("</Command>\n")

	// Build Arguments string with Windows quoting
	if len(spec.Args) > 0 {
		var argsBuf strings.Builder
		for i, arg := range spec.Args {
			if i > 0 {
				argsBuf.WriteString(" ")
			}
			argsBuf.WriteString(quoteWindowsArg(arg))
		}
		buf.WriteString("      <Arguments>")
		buf.WriteString(escapeXMLString(argsBuf.String()))
		buf.WriteString("</Arguments>\n")
	}

	buf.WriteString("    </Exec>\n")
	buf.WriteString("  </Actions>\n")

	buf.WriteString("</Task>\n")

	// Convert UTF-8 to UTF-16LE with BOM
	utf8Bytes := buf.Bytes()
	utf16Bytes := utf16.Encode([]rune(string(utf8Bytes)))

	// Add BOM
	result := make([]byte, 0, len(utf16Bytes)*2+2)
	result = append(result, 0xFF, 0xFE) // UTF-16LE BOM

	// Append UTF-16LE bytes
	for _, ch := range utf16Bytes {
		result = append(result, byte(ch), byte(ch>>8))
	}

	return result, nil
}

// TaskSchedulerBackend manages service lifecycle via Windows Task Scheduler.
type TaskSchedulerBackend struct {
	stateDir string
	runner   Runner
}

// Compile-time assertion that *TaskSchedulerBackend implements Manager.
var _ Manager = (*TaskSchedulerBackend)(nil)

// NewTaskSchedulerBackend creates a Task Scheduler backend.
// stateDir is where task XML files are stored and serves as the ownership record.
func NewTaskSchedulerBackend(stateDir string, runner Runner) *TaskSchedulerBackend {
	return &TaskSchedulerBackend{
		stateDir: stateDir,
		runner:   runner,
	}
}

// Install installs a service via Task Scheduler.
func (b *TaskSchedulerBackend) Install(ctx context.Context, spec Spec) error {
	// Validate label FIRST, before any side effects
	if err := ValidateLabel(spec.Label); err != nil {
		return err
	}

	if err := spec.Validate(); err != nil {
		return fmt.Errorf("invalid spec: %w", err)
	}

	xmlPath := filepath.Join(b.stateDir, spec.Label+".xml")

	// Check if XML exists and is owned
	if data, err := os.ReadFile(xmlPath); err == nil {
		if !taskXMLOwned(data) {
			return ErrNotOwned{Path: xmlPath}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read xml: %w", err)
	}

	// Render XML
	xmlData, err := renderTaskXML(spec)
	if err != nil {
		return fmt.Errorf("render xml: %w", err)
	}

	// Create stateDir
	if err := os.MkdirAll(b.stateDir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	// Write XML atomically
	_, err = atomicfile.Write(ctx, xmlPath, xmlData, 0o600)
	if err != nil {
		return fmt.Errorf("write xml: %w", err)
	}

	// Create task via schtasks
	taskName := "\\AntRouter\\" + spec.Label
	_, stderr, err := b.runner.Run(ctx, "schtasks", "/Create", "/TN", taskName, "/XML", xmlPath, "/F")
	if err != nil {
		return commandError("create task", err, stderr)
	}

	// Run the task immediately
	_, stderr, err = b.runner.Run(ctx, "schtasks", "/Run", "/TN", taskName)
	if err != nil {
		return commandError("run task", err, stderr)
	}

	return nil
}

// Uninstall uninstalls a service via Task Scheduler.
func (b *TaskSchedulerBackend) Uninstall(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}

	xmlPath := filepath.Join(b.stateDir, label+".xml")

	// Ownership is proven by our XML; unreadable paths get no side effects.
	data, err := os.ReadFile(xmlPath)
	existed := err == nil
	switch {
	case existed:
		if !taskXMLOwned(data) {
			return ErrNotOwned{Path: xmlPath}
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("read xml: %w", err)
	}

	taskName := "\\AntRouter\\" + label

	// Stopping a task that is not running fails; that is expected.
	_, _, _ = b.runner.Run(ctx, "schtasks", "/End", "/TN", taskName)

	// Without our XML the task is assumed absent, so a delete failure is
	// expected. With our XML a delete failure is real and the XML is kept so
	// ownership can still be proven on retry.
	_, stderr, err := b.runner.Run(ctx, "schtasks", "/Delete", "/TN", taskName, "/F")
	if err != nil && existed {
		return commandError("delete task", err, stderr)
	}

	// Remove XML file
	if err := os.Remove(xmlPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove xml: %w", err)
	}

	return nil
}

// Start starts a service via Task Scheduler.
func (b *TaskSchedulerBackend) Start(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}

	taskName := "\\AntRouter\\" + label
	_, stderr, err := b.runner.Run(ctx, "schtasks", "/Run", "/TN", taskName)
	if err != nil {
		return commandError("start task", err, stderr)
	}

	return nil
}

// Stop stops a service via Task Scheduler.
func (b *TaskSchedulerBackend) Stop(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}

	taskName := "\\AntRouter\\" + label
	_, stderr, err := b.runner.Run(ctx, "schtasks", "/End", "/TN", taskName)
	if err != nil {
		return commandError("stop task", err, stderr)
	}

	return nil
}

// Status returns the status of a service via Task Scheduler.
// If the XML file does not exist, returns StateNotInstalled without calling runner.
func (b *TaskSchedulerBackend) Status(ctx context.Context, label string) (Status, error) {
	if err := ValidateLabel(label); err != nil {
		return Status{}, err
	}

	xmlPath := filepath.Join(b.stateDir, label+".xml")
	if _, err := os.Stat(xmlPath); err != nil {
		if os.IsNotExist(err) {
			return Status{State: StateNotInstalled}, nil
		}
		return Status{}, fmt.Errorf("stat xml: %w", err)
	}

	// Query task status
	taskName := "\\AntRouter\\" + label
	stdout, stderr, err := b.runner.Run(ctx, "schtasks", "/Query", "/TN", taskName, "/FO", "CSV", "/NH")
	if err != nil {
		return Status{}, commandError("query task", err, stderr)
	}

	// Parse CSV
	reader := csv.NewReader(bytes.NewReader(stdout))
	records, err := reader.ReadAll()
	if err != nil {
		return Status{}, fmt.Errorf("parse csv: %w", err)
	}

	if len(records) == 0 {
		return Status{State: StateUnknown}, nil
	}

	record := records[0]
	if len(record) < 3 {
		return Status{State: StateUnknown}, nil
	}

	// Status is in the 3rd field (index 2)
	status := strings.TrimSpace(record[2])
	switch status {
	case "Running":
		return Status{State: StateRunning}, nil
	case "Ready":
		return Status{State: StateStopped}, nil
	case "Disabled":
		return Status{State: StateStopped, Detail: "disabled"}, nil
	default:
		// Non-English or unknown status
		return Status{State: StateUnknown, Detail: status}, nil
	}
}
