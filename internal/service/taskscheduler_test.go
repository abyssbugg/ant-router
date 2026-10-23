package service

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

// splitWindowsArgs is a faithful model of CommandLineToArgvW argument
// parsing (for arguments after the program name):
//   - whitespace (space, tab) outside quotes separates arguments;
//   - 2n backslashes followed by '"' yield n backslashes and toggle quoting;
//   - 2n+1 backslashes followed by '"' yield n backslashes and a literal '"';
//   - backslashes not followed by '"' are literal;
//   - inside quotes, '""' yields a literal '"' and quoting continues.
func splitWindowsArgs(cmdline string) []string {
	var args []string
	var cur strings.Builder
	inArg, inQuotes := false, false
	for i := 0; i < len(cmdline); {
		c := cmdline[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(cmdline) && cmdline[i] == '\\' {
				n++
				i++
			}
			inArg = true
			if i < len(cmdline) && cmdline[i] == '"' {
				cur.WriteString(strings.Repeat("\\", n/2))
				if n%2 == 1 {
					cur.WriteByte('"')
					i++
				}
				continue
			}
			cur.WriteString(strings.Repeat("\\", n))
		case c == '"':
			inArg = true
			if inQuotes && i+1 < len(cmdline) && cmdline[i+1] == '"' {
				cur.WriteByte('"')
				i += 2
				continue
			}
			inQuotes = !inQuotes
			i++
		case (c == ' ' || c == '\t') && !inQuotes:
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
			i++
		default:
			inArg = true
			cur.WriteByte(c)
			i++
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}

func TestSplitWindowsArgsModel(t *testing.T) {
	// Documented CommandLineToArgvW examples.
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`"abc" d e`, []string{"abc", "d", "e"}},
		{`a\\\b d"e f"g h`, []string{`a\\\b`, "de fg", "h"}},
		{`a\\\"b c d`, []string{`a\"b`, "c", "d"}},
		{`a\\\\"b c" d e`, []string{`a\\b c`, "d", "e"}},
		{`""`, []string{""}},
	} {
		got := splitWindowsArgs(tc.in)
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") || len(got) != len(tc.want) {
			t.Errorf("splitWindowsArgs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func joinWindowsArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteWindowsArg(a)
	}
	return strings.Join(quoted, " ")
}

func TestQuoteWindowsArgMultiRoundTrip(t *testing.T) {
	args := []string{"", `C:\Program Files\`, `a\\"b`, `\\`, `"`, `x y\`, "plain", `\"\\"`}
	got := splitWindowsArgs(joinWindowsArgs(args))
	if len(got) != len(args) {
		t.Fatalf("got %d args %q, want %q", len(got), got, args)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Errorf("arg %d: got %q, want %q (cmdline %q)", i, got[i], args[i], joinWindowsArgs(args))
		}
	}
}

func FuzzQuoteWindowsArg(f *testing.F) {
	for _, s := range []string{"", "a", "a b", `a"b`, `a\`, `a\\`, `a\"b`, `\\"`, `"\`, "x\ty"} {
		f.Add(s, "next")
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if hasControlCharacters(a) || hasControlCharacters(b) || strings.ContainsRune(a+b, 0) {
			t.Skip()
		}
		got := splitWindowsArgs(joinWindowsArgs([]string{a, b}))
		if len(got) != 2 || got[0] != a || got[1] != b {
			t.Fatalf("round trip of %q, %q via %q = %q", a, b, joinWindowsArgs([]string{a, b}), got)
		}
	})
}

// TestQuoteWindowsArgRoundTrip tests that quoting and splitting are inverses.
func TestQuoteWindowsArgRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		arg  string
	}{
		{"empty", ""},
		{"simple", "hello"},
		{"with space", "hello world"},
		{"with tab", "hello\tworld"},
		{"with quote", `hello"world`},
		{"trailing backslash", `path\`},
		{"double backslash", `path\\`},
		{"backslash before quote", `path\"file`},
		{"multiple backslashes", `path\\\\`},
		{"backslash before quote middle", `a\\b`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quoted := quoteWindowsArg(tt.arg)
			// Reconstruct command line
			cmdline := quoted
			parsed := splitWindowsArgs(cmdline)
			if len(parsed) != 1 {
				t.Fatalf("expected 1 arg, got %d: %v", len(parsed), parsed)
			}
			if parsed[0] != tt.arg {
				t.Errorf("round-trip failed: %q -> %q -> %q", tt.arg, quoted, parsed[0])
			}
		})
	}
}

// TestQuoteWindowsArgCorpus tests quoting with a corpus of seed values.
func TestQuoteWindowsArgCorpus(t *testing.T) {
	seeds := []string{
		"",
		"a",
		"abc",
		"a b",
		"a\tb",
		`a"b`,
		`a\b`,
		`a\\b`,
		`a\"b`,
		`a\`,
		`a\\`,
	}

	for _, input := range seeds {
		t.Run("corpus:"+input, func(t *testing.T) {
			// Skip inputs with NUL or control characters
			for _, ch := range input {
				if ch == 0 || (ch >= 0 && ch <= 0x1F) || ch == 0x7F {
					t.Skip("input contains NUL or control character")
				}
			}

			quoted := quoteWindowsArg(input)
			cmdline := quoted
			parsed := splitWindowsArgs(cmdline)

			if len(parsed) != 1 {
				t.Errorf("round-trip failed: expected 1 arg, got %d", len(parsed))
				return
			}

			if parsed[0] != input {
				t.Errorf("round-trip failed: %q -> %q -> %q", input, quoted, parsed[0])
			}
		})
	}
}

// TestRenderTaskXML tests XML generation with proper encoding.
// Helper types for testing

type fakeRunnerFunc func(ctx context.Context, name string, args ...string) ([]byte, []byte, error)

func (f fakeRunnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	return f(ctx, name, args...)
}

const testSID = "S-1-5-21-1111111111-2222222222-3333333333-1001"

// fakeTask is one registered task in fakeScheduler.
type fakeTask struct {
	state       int
	description string
	userSID     string
	xml         string
}

// fakeFailure makes one cmdlet fail; applied means its effect happens first
// (for example, a delete that removes the task but still reports an error).
type fakeFailure struct {
	err     error
	applied bool
	// once makes only the first matching call fail.
	once bool
}

// fakeScheduler models the ScheduledTasks cmdlets the backend sends through
// powershell -EncodedCommand. It decodes every script, so tests observe what
// the backend actually asks Windows to do.
type fakeScheduler struct {
	t     *testing.T
	mu    sync.Mutex
	tasks map[string]*fakeTask
	fail  map[string]fakeFailure
	ops   []string
	// beforeCall, if set, runs before each call outside mu, so a test can
	// hold one lifecycle operation in the middle of its call sequence.
	beforeCall func(op string)
}

func newFakeScheduler(t *testing.T) *fakeScheduler {
	return &fakeScheduler{t: t, tasks: map[string]*fakeTask{}, fail: map[string]fakeFailure{}}
}

var (
	cmdletPattern = regexp.MustCompile(`\b(Get|Export|Register|Start|Stop|Unregister)-ScheduledTask -TaskPath '\\AntRouter\\' -TaskName '([^']*)'`)
	xmlPattern    = regexp.MustCompile(`FromBase64String\('([A-Za-z0-9+/=]*)'\)`)
)

func decodeScript(t *testing.T, name string, args []string) string {
	t.Helper()
	want := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand"}
	if name != "powershell" || len(args) != len(want)+1 || strings.Join(args[:len(want)], " ") != strings.Join(want, " ") {
		t.Fatalf("command = %s %q, want powershell %q <script>", name, args, want)
	}
	raw, err := base64.StdEncoding.DecodeString(args[len(want)])
	if err != nil || len(raw)%2 != 0 {
		t.Fatalf("encoded command is not base64 UTF-16LE: %v", err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	script := string(utf16.Decode(units))
	if !strings.HasPrefix(script, "$ErrorActionPreference = 'Stop'\n") {
		t.Fatalf("script does not stop on errors:\n%s", script)
	}
	return script
}

func (f *fakeScheduler) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.t.Helper()
	script := decodeScript(f.t, name, args)
	match := cmdletPattern.FindStringSubmatch(script)
	if match == nil {
		f.t.Fatalf("script has no ScheduledTasks cmdlet:\n%s", script)
	}
	op, label := match[1], match[2]
	if f.beforeCall != nil {
		f.beforeCall(op)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, op+" "+label)
	failure, failing := f.fail[op]
	if failing && failure.once {
		delete(f.fail, op)
	}
	if failing && !failure.applied {
		return nil, []byte("synthetic " + op + " failure"), failure.err
	}
	task := f.tasks[label]
	var stdout string
	switch op {
	case "Get":
		if task == nil {
			stdout = "absent\r\n"
		} else {
			stdout = fmt.Sprintf("present\r\n%d\r\n%s\r\n%s\r\n.\r\n", task.state, task.userSID, task.description)
		}
	case "Export":
		if task == nil {
			return nil, []byte("task not found"), errors.New("exit status 1")
		}
		stdout = encodeUTF16Base64(task.xml) + "\r\n"
	case "Register":
		encoded := xmlPattern.FindStringSubmatch(script)
		if encoded == nil {
			f.t.Fatalf("Register script carries no XML:\n%s", script)
		}
		document := decodeUTF16Base64(f.t, encoded[1])
		parsed := parseTaskXML(f.t, document)
		previous := 3
		if task != nil {
			previous = task.state
		}
		f.tasks[label] = &fakeTask{state: previous, description: parsed.Registration.Description, userSID: parsed.Principal.UserID, xml: document}
	case "Start", "Stop":
		if task == nil {
			return nil, []byte("task not found"), errors.New("exit status 1")
		}
		task.state = map[string]int{"Start": 4, "Stop": 3}[op]
	case "Unregister":
		if task == nil {
			return nil, []byte("task not found"), errors.New("exit status 1")
		}
		delete(f.tasks, label)
	}
	if failing {
		return nil, []byte("synthetic " + op + " failure"), failure.err
	}
	return []byte(stdout), nil, nil
}

func (f *fakeScheduler) opList() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.ops, ", ")
}

func decodeUTF16Base64(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw)%2 != 0 {
		t.Fatalf("task XML is not base64 UTF-16LE: %v", err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}

type parsedTask struct {
	XMLName      xml.Name
	Version      string `xml:"version,attr"`
	Registration struct {
		Description string `xml:"Description"`
	} `xml:"RegistrationInfo"`
	Triggers struct {
		LogonTrigger struct {
			Enabled string `xml:"Enabled"`
			UserID  string `xml:"UserId"`
		} `xml:"LogonTrigger"`
	} `xml:"Triggers"`
	Principal struct {
		ID        string `xml:"id,attr"`
		UserID    string `xml:"UserId"`
		LogonType string `xml:"LogonType"`
		RunLevel  string `xml:"RunLevel"`
	} `xml:"Principals>Principal"`
	Settings struct {
		MultipleInstancesPolicy string `xml:"MultipleInstancesPolicy"`
		ExecutionTimeLimit      string `xml:"ExecutionTimeLimit"`
		RestartCount            string `xml:"RestartOnFailure>Count"`
	} `xml:"Settings"`
	Actions struct {
		Context string `xml:"Context,attr"`
		Exec    struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

func parseTaskXML(t *testing.T, document string) parsedTask {
	t.Helper()
	const decl = `<?xml version="1.0" encoding="UTF-16"?>`
	if !strings.HasPrefix(document, decl) {
		t.Fatalf("missing UTF-16 XML declaration: %q", document[:min(len(document), 60)])
	}
	var task parsedTask
	if err := xml.Unmarshal([]byte(strings.TrimPrefix(document, decl)), &task); err != nil {
		t.Fatalf("task XML does not parse: %v\n%s", err, document)
	}
	return task
}

func taskSpec(t *testing.T, args ...string) Spec {
	t.Helper()
	dir := t.TempDir()
	executable := filepath.Join(dir, "routerd.exe")
	if err := os.WriteFile(executable, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Spec{Label: "test-service", Executable: executable, Args: args, LogPath: filepath.Join(dir, "routerd.log")}
}

func newTestBackend(t *testing.T, runner Runner) *TaskSchedulerBackend {
	t.Helper()
	backend := NewTaskSchedulerBackend(t.TempDir(), runner)
	backend.currentUserSID = func() (string, error) { return testSID, nil }
	return backend
}

// previousTaskXML is the registered definition of an owned task that a
// reinstall replaces; a rollback must restore exactly this document.
var previousTaskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>` + OwnershipMarker + `</Description></RegistrationInfo>
  <Principals><Principal id="Author"><UserId>` + testSID + `</UserId></Principal></Principals>
  <Actions Context="Author"><Exec><Command>C:\previous\routerd.exe</Command></Exec></Actions>
</Task>`

func ownedTask(state int) *fakeTask {
	return &fakeTask{state: state, description: OwnershipMarker, userSID: testSID, xml: previousTaskXML}
}

func encodeUTF16Base64(text string) string {
	units := utf16.Encode([]rune(text))
	raw := make([]byte, 2*len(units))
	for i, unit := range units {
		raw[2*i], raw[2*i+1] = byte(unit), byte(unit>>8)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// A reinstall that cannot finish puts the previous definition back, and
// restarts the task if it was running, instead of leaving the new
// definition registered under a failed install.
func TestTaskSchedulerReinstallFailureRestoresPreviousDefinition(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   int
		failOp  string
		ops     []string
		wantRun int
	}{
		{"stop fails", 4, "Stop", []string{"Get test-service", "Export test-service", "Register test-service", "Stop test-service", "Register test-service"}, 4},
		{"start fails while running", 4, "Start", []string{"Get test-service", "Export test-service", "Register test-service", "Stop test-service", "Start test-service", "Register test-service", "Start test-service"}, 4},
		{"start fails while stopped", 3, "Start", []string{"Get test-service", "Export test-service", "Register test-service", "Stop test-service", "Start test-service", "Register test-service"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			scheduler.tasks["test-service"] = ownedTask(tc.state)
			scheduler.fail[tc.failOp] = fakeFailure{err: errors.New("exit status 1"), once: true}
			if err := newTestBackend(t, scheduler).Install(context.Background(), taskSpec(t)); err == nil {
				t.Fatal("Install succeeded, want the reinstall failure")
			}
			expectOps(t, scheduler, tc.ops...)
			task := scheduler.tasks["test-service"]
			if task == nil || task.xml != previousTaskXML || task.state != tc.wantRun {
				t.Fatalf("task after failed reinstall = %+v, want the previous definition in state %d", task, tc.wantRun)
			}
		})
	}
}

// A failed restore is reported together with the original failure.
func TestTaskSchedulerReinstallReportsRestoreFailure(t *testing.T) {
	scheduler := newFakeScheduler(t)
	scheduler.tasks["test-service"] = ownedTask(4)
	scheduler.fail["Stop"] = fakeFailure{err: errors.New("stop exit 1")}
	backend := newTestBackend(t, scheduler)
	calls := 0
	scheduler.beforeCall = func(op string) {
		if op == "Register" {
			calls++
			if calls == 2 {
				scheduler.mu.Lock()
				scheduler.fail["Register"] = fakeFailure{err: errors.New("restore exit 2")}
				scheduler.mu.Unlock()
			}
		}
	}
	err := backend.Install(context.Background(), taskSpec(t))
	if err == nil || !strings.Contains(err.Error(), "stop") || !strings.Contains(err.Error(), "restore previous task definition") {
		t.Fatalf("Install err = %v, want the stop failure joined with the restore failure", err)
	}
}

// Export failure aborts a reinstall before anything changes.
func TestTaskSchedulerReinstallExportFailureChangesNothing(t *testing.T) {
	scheduler := newFakeScheduler(t)
	scheduler.tasks["test-service"] = ownedTask(4)
	scheduler.fail["Export"] = fakeFailure{err: errors.New("exit status 1")}
	if err := newTestBackend(t, scheduler).Install(context.Background(), taskSpec(t)); err == nil {
		t.Fatal("Install succeeded despite export failure")
	}
	expectOps(t, scheduler, "Get test-service", "Export test-service")
}

func expectOps(t *testing.T, scheduler *fakeScheduler, want ...string) {
	t.Helper()
	if got := scheduler.opList(); got != strings.Join(want, ", ") {
		t.Fatalf("operations = [%s], want [%s]", got, strings.Join(want, ", "))
	}
}

func TestRenderTaskXMLDefinesAPerUserLogonTask(t *testing.T) {
	spec := taskSpec(t, "arg1", "arg with space", `quote"d`, `<tag>&amp;`, `trail\`)
	data, err := renderTaskXML(spec, testSID)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xFE {
		t.Fatal("missing UTF-16LE BOM")
	}
	units := make([]uint16, (len(data)-2)/2)
	for i := range units {
		units[i] = uint16(data[2+2*i]) | uint16(data[3+2*i])<<8
	}
	task := parseTaskXML(t, string(utf16.Decode(units)))
	if task.XMLName.Local != "Task" || task.Version != "1.2" {
		t.Fatalf("root = %s version %s, want Task 1.2", task.XMLName.Local, task.Version)
	}
	if task.Registration.Description != OwnershipMarker {
		t.Fatalf("description = %q, want exactly the ownership marker", task.Registration.Description)
	}
	if task.Triggers.LogonTrigger.Enabled != "true" || task.Triggers.LogonTrigger.UserID != testSID {
		t.Fatalf("logon trigger = %+v, want enabled and scoped to %s", task.Triggers.LogonTrigger, testSID)
	}
	if task.Principal.UserID != testSID || task.Principal.LogonType != "InteractiveToken" || task.Principal.RunLevel != "LeastPrivilege" || task.Principal.ID != task.Actions.Context {
		t.Fatalf("principal = %+v (actions context %q), want the installing user, interactive, least privilege", task.Principal, task.Actions.Context)
	}
	if task.Settings.MultipleInstancesPolicy != "IgnoreNew" || task.Settings.ExecutionTimeLimit != "PT0S" || task.Settings.RestartCount != "3" {
		t.Fatalf("settings = %+v", task.Settings)
	}
	if task.Actions.Exec.Command != spec.Executable {
		t.Fatalf("command = %q, want %q", task.Actions.Exec.Command, spec.Executable)
	}
	// Task Scheduler cannot redirect output, so the log path reaches routerd
	// as --log-file; the escaped, quoted arguments must parse back exactly.
	want := append(append([]string(nil), spec.Args...), "--log-file", spec.LogPath)
	if got := splitWindowsArgs(task.Actions.Exec.Arguments); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}

func TestRenderTaskXMLRejectsUnusableUserSID(t *testing.T) {
	spec := taskSpec(t)
	for _, sid := range []string{"", "S-1-5\n21", "S-1-5-21<x>"} {
		if _, err := renderTaskXML(spec, sid); err == nil {
			t.Fatalf("renderTaskXML accepted user SID %q", sid)
		}
	}
}

func TestTaskSchedulerInstallRegistersAndStartsANewTask(t *testing.T) {
	scheduler := newFakeScheduler(t)
	spec := taskSpec(t, "--instance", "dev")
	if err := newTestBackend(t, scheduler).Install(context.Background(), spec); err != nil {
		t.Fatalf("Install: %v", err)
	}
	expectOps(t, scheduler, "Get test-service", "Register test-service", "Start test-service")
	task := scheduler.tasks["test-service"]
	if task == nil || task.state != 4 || task.description != OwnershipMarker {
		t.Fatalf("task = %+v, want an owned running task", task)
	}
	parsed := parseTaskXML(t, task.xml)
	if got := splitWindowsArgs(parsed.Actions.Exec.Arguments); strings.Join(got, " ") != "--instance dev --log-file "+spec.LogPath {
		t.Fatalf("registered arguments = %q", got)
	}
}

func TestTaskSchedulerInstallReplacesItsOwnTask(t *testing.T) {
	scheduler := newFakeScheduler(t)
	scheduler.tasks["test-service"] = ownedTask(4)
	if err := newTestBackend(t, scheduler).Install(context.Background(), taskSpec(t)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// The running instance is stopped so the new definition takes effect
	// (MultipleInstancesPolicy IgnoreNew would ignore the start otherwise).
	expectOps(t, scheduler, "Get test-service", "Export test-service", "Register test-service", "Stop test-service", "Start test-service")
	if task := scheduler.tasks["test-service"]; task.state != 4 {
		t.Fatalf("task state = %d, want running", task.state)
	}
}

// Task Scheduler folders are machine-wide: a task carrying the marker but
// running as another user (or as no specific user) is foreign.
func TestTaskSchedulerRefusesAnotherUsersTask(t *testing.T) {
	for name, sid := range map[string]string{"other user": "S-1-5-21-1111111111-2222222222-3333333333-1002", "no principal": ""} {
		t.Run(name, func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			scheduler.tasks["test-service"] = &fakeTask{state: 4, description: OwnershipMarker, userSID: sid}
			backend := newTestBackend(t, scheduler)
			ctx := context.Background()
			var notOwned ErrNotOwned
			for op, err := range map[string]error{
				"Install":   backend.Install(ctx, taskSpec(t)),
				"Uninstall": backend.Uninstall(ctx, "test-service"),
				"Start":     backend.Start(ctx, "test-service"),
				"Stop":      backend.Stop(ctx, "test-service"),
			} {
				if !errors.As(err, &notOwned) {
					t.Errorf("%s err = %v, want ErrNotOwned", op, err)
				}
			}
			if _, err := backend.Status(ctx, "test-service"); !errors.As(err, &notOwned) {
				t.Errorf("Status err = %v, want ErrNotOwned", err)
			}
			for _, op := range scheduler.ops {
				if !strings.HasPrefix(op, "Get ") {
					t.Fatalf("foreign task was changed: %v", scheduler.ops)
				}
			}
		})
	}
}

// A same-named task without the ownership marker is never replaced.
func TestTaskSchedulerInstallRefusesAForeignTask(t *testing.T) {
	for name, description := range map[string]string{"other tool": "someone else's task", "marker as data": "see " + OwnershipMarker, "empty": ""} {
		t.Run(name, func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			scheduler.tasks["test-service"] = &fakeTask{state: 3, description: description}
			err := newTestBackend(t, scheduler).Install(context.Background(), taskSpec(t))
			var notOwned ErrNotOwned
			if !errors.As(err, &notOwned) || notOwned.Path != `\AntRouter\test-service` {
				t.Fatalf("Install err = %v, want ErrNotOwned for the task", err)
			}
			expectOps(t, scheduler, "Get test-service")
			if scheduler.tasks["test-service"].description != description {
				t.Fatal("foreign task was modified")
			}
		})
	}
}

func TestTaskSchedulerInstallFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing *fakeTask
		failOp   string
		wantOps  []string
		present  bool
	}{
		{"query fails", nil, "Get", []string{"Get test-service"}, false},
		{"register fails", nil, "Register", []string{"Get test-service", "Register test-service"}, false},
		// A fresh task that cannot start is rolled back: its logon trigger
		// would otherwise launch the service the caller saw fail to install.
		{"start fails on fresh install", nil, "Start", []string{"Get test-service", "Register test-service", "Start test-service", "Unregister test-service"}, false},
		// A reinstall keeps the (updated) owned task: there is no previous
		// state to restore, and removing it would uninstall the service.
		{"start fails on reinstall", ownedTask(3), "Start", []string{"Get test-service", "Export test-service", "Register test-service", "Stop test-service", "Start test-service", "Register test-service"}, true},
		{"stop fails on reinstall", ownedTask(4), "Stop", []string{"Get test-service", "Export test-service", "Register test-service", "Stop test-service", "Register test-service"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			if tc.existing != nil {
				scheduler.tasks["test-service"] = tc.existing
			}
			scheduler.fail[tc.failOp] = fakeFailure{err: errors.New("exit status 1")}
			err := newTestBackend(t, scheduler).Install(context.Background(), taskSpec(t))
			if err == nil || !strings.Contains(err.Error(), "synthetic "+tc.failOp+" failure") {
				t.Fatalf("Install err = %v, want the %s failure", err, tc.failOp)
			}
			expectOps(t, scheduler, tc.wantOps...)
			if _, present := scheduler.tasks["test-service"]; present != tc.present {
				t.Fatalf("task present = %v, want %v", present, tc.present)
			}
		})
	}
}

func TestTaskSchedulerInstallRollbackFailureIsReported(t *testing.T) {
	scheduler := newFakeScheduler(t)
	scheduler.fail["Start"] = fakeFailure{err: errors.New("exit status 1")}
	scheduler.fail["Unregister"] = fakeFailure{err: errors.New("exit status 2")}
	err := newTestBackend(t, scheduler).Install(context.Background(), taskSpec(t))
	if err == nil || !strings.Contains(err.Error(), "synthetic Start failure") || !strings.Contains(err.Error(), "synthetic Unregister failure") {
		t.Fatalf("Install err = %v, want both the start and the rollback failure", err)
	}
}

func TestTaskSchedulerInstallUserLookupFailure(t *testing.T) {
	scheduler := newFakeScheduler(t)
	backend := newTestBackend(t, scheduler)
	backend.currentUserSID = func() (string, error) { return "", errors.New("no user") }
	if err := backend.Install(context.Background(), taskSpec(t)); err == nil || !strings.Contains(err.Error(), "no user") {
		t.Fatalf("Install err = %v, want the user lookup failure", err)
	}
	expectOps(t, scheduler)
}

func TestTaskSchedulerInstallRejectsOversizedDefinition(t *testing.T) {
	scheduler := newFakeScheduler(t)
	long := strings.Repeat("a", 4000)
	err := newTestBackend(t, scheduler).Install(context.Background(), taskSpec(t, long, long, long))
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("Install err = %v, want a size error", err)
	}
	expectOps(t, scheduler)
}

func TestTaskSchedulerUninstall(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing *fakeTask
		fail     map[string]fakeFailure
		wantErr  string
		wantOps  []string
		present  bool
	}{
		{"owned running task", ownedTask(4), nil, "", []string{"Get test-service", "Stop test-service", "Unregister test-service"}, false},
		// Without a task there is nothing to do and nothing is touched.
		{"absent", nil, nil, "", []string{"Get test-service"}, false},
		{"foreign", &fakeTask{state: 4, description: "other"}, nil, "not owned", []string{"Get test-service"}, true},
		{"query fails", ownedTask(4), map[string]fakeFailure{"Get": {err: errors.New("exit status 1")}}, "synthetic Get failure", []string{"Get test-service"}, true},
		// A task that cannot be stopped is kept, so its process is never
		// orphaned without a definition.
		{"stop fails", ownedTask(4), map[string]fakeFailure{"Stop": {err: errors.New("exit status 1")}}, "synthetic Stop failure", []string{"Get test-service", "Stop test-service"}, true},
		{"unregister fails", ownedTask(3), map[string]fakeFailure{"Unregister": {err: errors.New("exit status 1")}}, "synthetic Unregister failure", []string{"Get test-service", "Stop test-service", "Unregister test-service", "Get test-service"}, true},
		// A delete that removed the task but reported an error is complete.
		{"unregister removed but failed", ownedTask(3), map[string]fakeFailure{"Unregister": {err: errors.New("timeout"), applied: true}}, "", []string{"Get test-service", "Stop test-service", "Unregister test-service", "Get test-service"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			if tc.existing != nil {
				scheduler.tasks["test-service"] = tc.existing
			}
			for op, failure := range tc.fail {
				scheduler.fail[op] = failure
			}
			err := newTestBackend(t, scheduler).Uninstall(context.Background(), "test-service")
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("Uninstall err = %v, want %q", err, tc.wantErr)
			}
			expectOps(t, scheduler, tc.wantOps...)
			if _, present := scheduler.tasks["test-service"]; present != tc.present {
				t.Fatalf("task present = %v, want %v", present, tc.present)
			}
		})
	}
}

func TestTaskSchedulerStartAndStopRequireOwnership(t *testing.T) {
	type op func(*TaskSchedulerBackend) error
	for name, run := range map[string]op{
		"Start": func(b *TaskSchedulerBackend) error { return b.Start(context.Background(), "test-service") },
		"Stop":  func(b *TaskSchedulerBackend) error { return b.Stop(context.Background(), "test-service") },
	} {
		t.Run(name+" owned", func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			scheduler.tasks["test-service"] = ownedTask(3)
			if err := run(newTestBackend(t, scheduler)); err != nil {
				t.Fatal(err)
			}
			expectOps(t, scheduler, "Get test-service", name+" test-service")
		})
		t.Run(name+" absent", func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			var missing ErrNotInstalled
			if err := run(newTestBackend(t, scheduler)); !errors.As(err, &missing) {
				t.Fatalf("err = %v, want ErrNotInstalled", err)
			}
			expectOps(t, scheduler, "Get test-service")
		})
		t.Run(name+" foreign", func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			scheduler.tasks["test-service"] = &fakeTask{state: 3, description: "other"}
			var notOwned ErrNotOwned
			if err := run(newTestBackend(t, scheduler)); !errors.As(err, &notOwned) {
				t.Fatalf("err = %v, want ErrNotOwned", err)
			}
			expectOps(t, scheduler, "Get test-service")
		})
		t.Run(name+" fails", func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			scheduler.tasks["test-service"] = ownedTask(3)
			scheduler.fail[name] = fakeFailure{err: errors.New("exit status 1")}
			if err := run(newTestBackend(t, scheduler)); err == nil || !strings.Contains(err.Error(), "synthetic "+name+" failure") {
				t.Fatalf("err = %v, want the %s failure", err, name)
			}
		})
	}
}

// Task state comes from the ScheduledTasks State enum value, which is not
// localized, rather than from schtasks' display text.
func TestTaskSchedulerStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		task *fakeTask
		want Status
	}{
		{"absent", nil, Status{State: StateNotInstalled}},
		{"running", ownedTask(4), Status{State: StateRunning}},
		{"ready", ownedTask(3), Status{State: StateStopped}},
		{"queued", ownedTask(2), Status{State: StateUnknown, Detail: "queued"}},
		{"disabled", ownedTask(1), Status{State: StateStopped, Detail: "disabled"}},
		{"unknown", ownedTask(0), Status{State: StateUnknown}},
		{"unexpected", ownedTask(9), Status{State: StateUnknown, Detail: "state 9"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			if tc.task != nil {
				scheduler.tasks["test-service"] = tc.task
			}
			got, err := newTestBackend(t, scheduler).Status(context.Background(), "test-service")
			if err != nil || got != tc.want {
				t.Fatalf("Status = %+v, %v; want %+v", got, err, tc.want)
			}
			expectOps(t, scheduler, "Get test-service")
		})
	}
}

func TestTaskSchedulerStatusFailures(t *testing.T) {
	t.Run("foreign", func(t *testing.T) {
		scheduler := newFakeScheduler(t)
		scheduler.tasks["test-service"] = &fakeTask{state: 4, description: "other"}
		var notOwned ErrNotOwned
		if _, err := newTestBackend(t, scheduler).Status(context.Background(), "test-service"); !errors.As(err, &notOwned) {
			t.Fatalf("Status err = %v, want ErrNotOwned", err)
		}
	})
	t.Run("query fails", func(t *testing.T) {
		scheduler := newFakeScheduler(t)
		scheduler.fail["Get"] = fakeFailure{err: errors.New("exit status 1")}
		if _, err := newTestBackend(t, scheduler).Status(context.Background(), "test-service"); err == nil || !strings.Contains(err.Error(), "synthetic Get failure") {
			t.Fatalf("Status err = %v, want the query failure", err)
		}
	})
	for name, stdout := range map[string]string{"empty": "", "garbage": "maybe\r\n", "bad state": "present\r\nrunning\r\n" + testSID + "\r\n" + OwnershipMarker + "\r\n.\r\n", "truncated": "present\r\n", "no terminator": "present\r\n3\r\n" + testSID + "\r\n" + OwnershipMarker + "\r\n"} {
		t.Run("malformed "+name, func(t *testing.T) {
			runner := fakeRunnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) { return []byte(stdout), nil, nil })
			_, err := newTestBackend(t, runner).Status(context.Background(), "test-service")
			if err == nil {
				t.Fatalf("Status accepted query output %q", stdout)
			}
			// Well-formed except for the state: must fail in state parsing.
			if name == "bad state" && !strings.Contains(err.Error(), "unexpected state") {
				t.Fatalf("Status err = %v, want an unexpected-state error", err)
			}
		})
	}
}

// Lifecycle operations on one label never interleave their scheduler calls:
// with Install paused between registering and starting the task, a
// concurrent Uninstall makes no call until Install finishes.
func TestTaskSchedulerSerializesLifecycleOperations(t *testing.T) {
	scheduler := newFakeScheduler(t)
	backend := newTestBackend(t, scheduler)
	spec := taskSpec(t)
	paused, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	scheduler.beforeCall = func(op string) {
		if op == "Start" {
			once.Do(func() { close(paused); <-release })
		}
	}
	installed := make(chan error, 1)
	go func() { installed <- backend.Install(context.Background(), spec) }()
	<-paused
	uninstalled := make(chan error, 1)
	go func() { uninstalled <- backend.Uninstall(context.Background(), spec.Label) }()
	// Without the lock Uninstall's first query would be recorded at once.
	time.Sleep(200 * time.Millisecond)
	expectOps(t, scheduler, "Get test-service", "Register test-service")
	close(release)
	if err := <-installed; err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := <-uninstalled; err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	expectOps(t, scheduler, "Get test-service", "Register test-service", "Start test-service", "Get test-service", "Stop test-service", "Unregister test-service")
}

func TestUnsafeLabelsRejected(t *testing.T) {
	for _, label := range []string{"", "invalid..label", "_starts_with_underscore", "label-with-ñ", "label\nwith\nnewline", "quote'd"} {
		t.Run("label:"+label, func(t *testing.T) {
			scheduler := newFakeScheduler(t)
			backend := newTestBackend(t, scheduler)
			spec := taskSpec(t)
			spec.Label = label
			ctx := context.Background()
			if backend.Install(ctx, spec) == nil || backend.Uninstall(ctx, label) == nil || backend.Start(ctx, label) == nil || backend.Stop(ctx, label) == nil {
				t.Fatal("lifecycle operation accepted an unsafe label")
			}
			if _, err := backend.Status(ctx, label); err == nil {
				t.Fatal("Status accepted an unsafe label")
			}
			expectOps(t, scheduler)
		})
	}
}

// The Task Scheduler backend's PowerShell is the system copy, resolved from
// SystemRoot rather than PATH, so a planted powershell.exe is never run.
func TestServiceCommandBuildsSystemPowerShell(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SystemRoot", root)
	cmd, err := serviceCommand(context.Background(), "powershell", "-NoLogo")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if cmd.Path != want || len(cmd.Args) != 2 || cmd.Args[1] != "-NoLogo" {
		t.Fatalf("serviceCommand(powershell) = %q %q, want %q", cmd.Path, cmd.Args, want)
	}
	for _, value := range []string{"", "relative"} {
		t.Setenv("SystemRoot", value)
		if _, err := serviceCommand(context.Background(), "powershell"); err == nil {
			t.Fatalf("serviceCommand(powershell) accepted SystemRoot %q", value)
		}
	}
}
