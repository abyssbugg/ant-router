package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
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

// taskFolder is the Task Scheduler folder holding every task this package
// manages; Register-ScheduledTask creates it on first use.
const taskFolder = `\AntRouter\`

// maxEncodedCommand bounds the -EncodedCommand argument well below the
// 32767-character Windows command-line limit.
const maxEncodedCommand = 30000

// sidPattern matches a Windows security identifier string.
var sidPattern = regexp.MustCompile(`^S-[0-9]+(-[0-9]+)+$`)

func taskName(label string) string { return taskFolder + label }

// renderTaskXML renders a Task Scheduler 1.2 definition for spec, encoded as
// UTF-16LE with a BOM. The task runs as, and starts at the logon of, the
// installing user identified by userSID. Task Scheduler cannot redirect a
// task's output, so spec.LogPath reaches the service as "--log-file PATH".
// RegistrationInfo/Description is exactly OwnershipMarker: it is the
// ownership proof checked before every lifecycle operation.
func renderTaskXML(spec Spec, userSID string) ([]byte, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if !sidPattern.MatchString(userSID) {
		return nil, fmt.Errorf("current user SID %q is not a Windows security identifier", userSID)
	}
	user := escapeXMLString(userSID)
	args := append(append([]string(nil), spec.Args...), "--log-file", spec.LogPath)
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteWindowsArg(arg)
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\n")
	buf.WriteString(`<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` + "\n")
	buf.WriteString("  <RegistrationInfo>\n")
	buf.WriteString("    <Description>" + escapeXMLString(OwnershipMarker) + "</Description>\n")
	buf.WriteString("  </RegistrationInfo>\n")
	buf.WriteString("  <Triggers>\n")
	buf.WriteString("    <LogonTrigger>\n")
	buf.WriteString("      <Enabled>true</Enabled>\n")
	buf.WriteString("      <UserId>" + user + "</UserId>\n")
	buf.WriteString("    </LogonTrigger>\n")
	buf.WriteString("  </Triggers>\n")
	buf.WriteString("  <Principals>\n")
	buf.WriteString("    <Principal id=\"Author\">\n")
	buf.WriteString("      <UserId>" + user + "</UserId>\n")
	buf.WriteString("      <LogonType>InteractiveToken</LogonType>\n")
	buf.WriteString("      <RunLevel>LeastPrivilege</RunLevel>\n")
	buf.WriteString("    </Principal>\n")
	buf.WriteString("  </Principals>\n")
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
	buf.WriteString("  <Actions Context=\"Author\">\n")
	buf.WriteString("    <Exec>\n")
	buf.WriteString("      <Command>" + escapeXMLString(spec.Executable) + "</Command>\n")
	buf.WriteString("      <Arguments>" + escapeXMLString(strings.Join(quoted, " ")) + "</Arguments>\n")
	buf.WriteString("    </Exec>\n")
	buf.WriteString("  </Actions>\n")
	buf.WriteString("</Task>\n")

	return append([]byte{0xFF, 0xFE}, utf16LE(buf.String())...), nil
}

func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2*len(units))
	for _, unit := range units {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return out
}

// psQuote renders s as a PowerShell single-quoted string literal.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// taskRef is the cmdlet parameters naming the task for label.
func taskRef(label string) string {
	return "-TaskPath " + psQuote(taskFolder) + " -TaskName " + psQuote(label)
}

// encodePowerShell returns the -EncodedCommand form of a script that stops on
// the first error. The base64 argument needs no command-line quoting.
func encodePowerShell(script string) (string, error) {
	encoded := base64.StdEncoding.EncodeToString(utf16LE("$ErrorActionPreference = 'Stop'\n$ProgressPreference = 'SilentlyContinue'\n" + script))
	if len(encoded) > maxEncodedCommand {
		return "", fmt.Errorf("task definition too large for the PowerShell command line (%d > %d)", len(encoded), maxEncodedCommand)
	}
	return encoded, nil
}

// TaskSchedulerBackend manages a per-user service as a Windows Task
// Scheduler task through the ScheduledTasks PowerShell cmdlets. Their task
// State enum is locale-independent, unlike schtasks' display text, and
// Stop-ScheduledTask succeeds for a task that is not running.
//
// The registered task's own definition is the ownership record: every
// operation first queries the task and acts only when its description is
// exactly OwnershipMarker. Lifecycle operations for one label are serialized
// with a lock file in stateDir.
type TaskSchedulerBackend struct {
	stateDir       string
	runner         Runner
	currentUserSID func() (string, error)
}

// Compile-time assertion that *TaskSchedulerBackend implements Manager.
var _ Manager = (*TaskSchedulerBackend)(nil)

// NewTaskSchedulerBackend creates a Task Scheduler backend. stateDir holds the
// per-label lifecycle lock files.
func NewTaskSchedulerBackend(stateDir string, runner Runner) *TaskSchedulerBackend {
	return &TaskSchedulerBackend{stateDir: stateDir, runner: runner, currentUserSID: currentUserSID}
}

// currentUserSID returns the SID of the current Windows user (user.Current
// reports the SID as Uid on Windows).
func currentUserSID() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("look up current user: %w", err)
	}
	return current.Uid, nil
}

func (b *TaskSchedulerBackend) powershell(ctx context.Context, encoded string) ([]byte, []byte, error) {
	return b.runner.Run(ctx, "powershell", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
}

// script runs one cmdlet script, returning its output or a command error.
func (b *TaskSchedulerBackend) script(ctx context.Context, op, script string) ([]byte, error) {
	encoded, err := encodePowerShell(script)
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := b.powershell(ctx, encoded)
	if err != nil {
		return nil, commandError(op, err, stderr)
	}
	return stdout, nil
}

// taskInfo is the queried state of the task for one label.
type taskInfo struct {
	present bool
	owned   bool
	state   int
}

// queryScript prints "absent", or "present", the State enum value, the SID
// of the task's principal (translated from an account name if needed; empty
// if it has none), the Description lines, and a final "." so an empty
// Description is still distinguishable from truncated output. A missing
// task is recognized by the error category, which is not localized.
func queryScript(label string) string {
	return `try { $task = Get-ScheduledTask ` + taskRef(label) + ` -ErrorAction Stop }
catch { if ($_.CategoryInfo.Category -eq 'ObjectNotFound') { 'absent'; exit 0 }; throw }
$user = [string]$task.Principal.UserId
$sid = ''
if ($user -match '^S-[0-9]+(-[0-9]+)+$') { $sid = $user }
elseif ($user) { try { $sid = ([Security.Principal.NTAccount]$user).Translate([Security.Principal.SecurityIdentifier]).Value } catch { $sid = '' } }
'present'
[int]$task.State
$sid
[string]$task.Description
'.'
`
}

// query reports whether the task exists, whether it is owned, and its State
// enum value. The task is owned only if its Description is exactly
// OwnershipMarker and its principal is the current user: Task Scheduler
// folders are machine-wide, so another user's same-named task is foreign.
func (b *TaskSchedulerBackend) query(ctx context.Context, label string) (taskInfo, error) {
	sid, err := b.currentUserSID()
	if err != nil {
		return taskInfo{}, fmt.Errorf("query task: %w", err)
	}
	stdout, err := b.script(ctx, "query task", queryScript(label))
	if err != nil {
		return taskInfo{}, err
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(stdout), "\r\n", "\n"), "\n"), "\n")
	switch {
	case len(lines) == 1 && lines[0] == "absent":
		return taskInfo{}, nil
	case len(lines) >= 4 && lines[0] == "present" && lines[len(lines)-1] == ".":
		state, err := strconv.Atoi(lines[1])
		if err != nil {
			return taskInfo{}, fmt.Errorf("query task: unexpected state %q", lines[1])
		}
		description := strings.Join(lines[3:len(lines)-1], "\n")
		return taskInfo{present: true, owned: description == OwnershipMarker && strings.EqualFold(lines[2], sid), state: state}, nil
	default:
		return taskInfo{}, fmt.Errorf("query task: unexpected output %q", truncateForError(stdout))
	}
}

func truncateForError(b []byte) string {
	const limit = 128
	if len(b) > limit {
		b = b[:limit]
	}
	return string(b)
}

// ownedTask queries the task for label and requires that it exists and is
// owned.
func (b *TaskSchedulerBackend) ownedTask(ctx context.Context, label string) (taskInfo, error) {
	info, err := b.query(ctx, label)
	switch {
	case err != nil:
		return taskInfo{}, err
	case !info.present:
		return taskInfo{}, ErrNotInstalled{Path: taskName(label)}
	case !info.owned:
		return taskInfo{}, ErrNotOwned{Path: taskName(label)}
	}
	return info, nil
}

// Install registers (or replaces its own) task and starts it. A same-named
// task without the ownership marker is never replaced. A newly registered
// task that fails to start is removed again so its logon trigger cannot
// launch a service whose installation failed.
func (b *TaskSchedulerBackend) Install(ctx context.Context, spec Spec) error {
	if err := ValidateLabel(spec.Label); err != nil {
		return err
	}
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("invalid spec: %w", err)
	}
	sid, err := b.currentUserSID()
	if err != nil {
		return fmt.Errorf("install task: %w", err)
	}
	definition, err := renderTaskXML(spec, sid)
	if err != nil {
		return fmt.Errorf("render task: %w", err)
	}
	register := `$xml = [Text.Encoding]::Unicode.GetString([Convert]::FromBase64String('` + base64.StdEncoding.EncodeToString(definition[2:]) + `'))
Register-ScheduledTask ` + taskRef(spec.Label) + ` -Xml $xml -Force | Out-Null
`
	// Size-check the definition before touching Task Scheduler.
	if _, err := encodePowerShell(register); err != nil {
		return err
	}

	lock, err := lockLabel(ctx, b.stateDir, spec.Label)
	if err != nil {
		return err
	}
	defer lock.Release()

	info, err := b.query(ctx, spec.Label)
	if err != nil {
		return err
	}
	if info.present && !info.owned {
		return ErrNotOwned{Path: taskName(spec.Label)}
	}
	if _, err := b.script(ctx, "register task", register); err != nil {
		return err
	}
	// Re-registering does not stop a running instance, and with
	// MultipleInstancesPolicy IgnoreNew the start below would leave the old
	// process running the previous definition, so stop it first.
	if info.present {
		if _, err := b.script(ctx, "stop task", "Stop-ScheduledTask "+taskRef(spec.Label)+"\n"); err != nil {
			return err
		}
	}
	if _, err := b.script(ctx, "start task", "Start-ScheduledTask "+taskRef(spec.Label)+"\n"); err != nil {
		if info.present {
			return err
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		if _, undoErr := b.script(cleanupCtx, "undo register", "Unregister-ScheduledTask "+taskRef(spec.Label)+" -Confirm:$false\n"); undoErr != nil {
			return errors.Join(err, undoErr)
		}
		return err
	}
	return nil
}

// Uninstall stops and removes the owned task. A missing task is already
// uninstalled; a foreign one is refused. A task that cannot be stopped is
// kept so its process is never left running without a definition.
func (b *TaskSchedulerBackend) Uninstall(ctx context.Context, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	lock, err := lockLabel(ctx, b.stateDir, label)
	if err != nil {
		return err
	}
	defer lock.Release()

	if _, err := b.ownedTask(ctx, label); err != nil {
		var missing ErrNotInstalled
		if errors.As(err, &missing) {
			return nil
		}
		return err
	}
	if _, err := b.script(ctx, "stop task", "Stop-ScheduledTask "+taskRef(label)+"\n"); err != nil {
		return err
	}
	if _, err := b.script(ctx, "delete task", "Unregister-ScheduledTask "+taskRef(label)+" -Confirm:$false\n"); err != nil {
		// The delete may have taken effect before failing (for example, a
		// timeout); only a task that is still registered is a failure.
		info, queryErr := b.query(ctx, label)
		if queryErr == nil && !info.present {
			return nil
		}
		return errors.Join(err, queryErr)
	}
	return nil
}

// Start runs the owned task.
func (b *TaskSchedulerBackend) Start(ctx context.Context, label string) error {
	return b.lifecycle(ctx, label, "start task", "Start-ScheduledTask")
}

// Stop ends the owned task; stopping a task that is not running succeeds.
func (b *TaskSchedulerBackend) Stop(ctx context.Context, label string) error {
	return b.lifecycle(ctx, label, "stop task", "Stop-ScheduledTask")
}

func (b *TaskSchedulerBackend) lifecycle(ctx context.Context, label, op, cmdlet string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	lock, err := lockLabel(ctx, b.stateDir, label)
	if err != nil {
		return err
	}
	defer lock.Release()
	if _, err := b.ownedTask(ctx, label); err != nil {
		return err
	}
	_, err = b.script(ctx, op, cmdlet+" "+taskRef(label)+"\n")
	return err
}

// ScheduledTasks StateEnum values.
const (
	taskStateUnknown  = 0
	taskStateDisabled = 1
	taskStateQueued   = 2
	taskStateReady    = 3
	taskStateRunning  = 4
)

// Status reports the state of the owned task from its State enum value.
func (b *TaskSchedulerBackend) Status(ctx context.Context, label string) (Status, error) {
	if err := ValidateLabel(label); err != nil {
		return Status{}, err
	}
	info, err := b.ownedTask(ctx, label)
	if err != nil {
		var missing ErrNotInstalled
		if errors.As(err, &missing) {
			return Status{State: StateNotInstalled}, nil
		}
		return Status{}, err
	}
	switch info.state {
	case taskStateRunning:
		return Status{State: StateRunning}, nil
	case taskStateReady:
		return Status{State: StateStopped}, nil
	case taskStateDisabled:
		return Status{State: StateStopped, Detail: "disabled"}, nil
	case taskStateQueued:
		return Status{State: StateUnknown, Detail: "queued"}, nil
	case taskStateUnknown:
		return Status{State: StateUnknown}, nil
	default:
		return Status{State: StateUnknown, Detail: fmt.Sprintf("state %d", info.state)}, nil
	}
}
