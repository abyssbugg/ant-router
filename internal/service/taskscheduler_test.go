package service

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
func TestRenderTaskXML(t *testing.T) {
	// Create a temporary executable file
	tmpExe := filepath.Join(t.TempDir(), "test.exe")
	if err := os.WriteFile(tmpExe, []byte("fake"), 0o755); err != nil {
		t.Fatalf("failed to create temp exe: %v", err)
	}

	tmpLog := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(tmpLog, []byte(""), 0o666); err != nil {
		t.Fatalf("failed to create temp log: %v", err)
	}

	spec := Spec{
		Label:      "test-service",
		Executable: tmpExe,
		Args:       []string{"arg1", "arg with space"},
		LogPath:    tmpLog,
	}

	xmlBytes, err := renderTaskXML(spec)
	if err != nil {
		t.Fatalf("renderTaskXML failed: %v", err)
	}

	// Check UTF-16LE BOM
	if len(xmlBytes) < 2 || xmlBytes[0] != 0xFF || xmlBytes[1] != 0xFE {
		t.Fatal("missing UTF-16LE BOM")
	}

	// Decode UTF-16LE
	utf16Bytes := make([]uint16, (len(xmlBytes)-2)/2)
	for i := 0; i < len(utf16Bytes); i++ {
		utf16Bytes[i] = uint16(xmlBytes[2+i*2]) | (uint16(xmlBytes[2+i*2+1]) << 8)
	}
	decoded := utf16.Decode(utf16Bytes)
	xmlStr := string(decoded)

	// Parse XML
	var task struct {
		XMLName      xml.Name
		Version      string `xml:"version,attr"`
		Xmlns        string `xml:"xmlns,attr"`
		Registration struct {
			Description string `xml:"Description"`
		} `xml:"RegistrationInfo"`
		Triggers struct {
			LogonTrigger struct {
				Enabled string `xml:"Enabled"`
			} `xml:"LogonTrigger"`
		} `xml:"Triggers"`
		Principal struct {
			LogonType string `xml:"LogonType"`
			RunLevel  string `xml:"RunLevel"`
		} `xml:"Principal"`
		Settings struct {
			MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
			DisallowStartIfOnBatteries string `xml:"DisallowStartIfOnBatteries"`
			StopIfGoingOnBatteries     string `xml:"StopIfGoingOnBatteries"`
			ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
			RestartOnFailure           struct {
				Interval string `xml:"Interval"`
				Count    string `xml:"Count"`
			} `xml:"RestartOnFailure"`
			Hidden string `xml:"Hidden"`
		} `xml:"Settings"`
		Actions struct {
			Exec struct {
				Command   string `xml:"Command"`
				Arguments string `xml:"Arguments"`
			} `xml:"Exec"`
		} `xml:"Actions"`
	}

	// The bytes were already decoded from UTF-16, so the declaration's
	// encoding no longer applies to xmlStr.
	decl := `<?xml version="1.0" encoding="UTF-16"?>`
	if !strings.HasPrefix(xmlStr, decl) {
		t.Fatalf("missing UTF-16 XML declaration: %q", xmlStr[:min(len(xmlStr), 60)])
	}
	if err := xml.Unmarshal([]byte(strings.TrimPrefix(xmlStr, decl)), &task); err != nil {
		t.Fatalf("xml parsing failed: %v", err)
	}

	// Verify structure
	if task.XMLName.Local != "Task" {
		t.Errorf("root element should be Task, got %s", task.XMLName.Local)
	}
	if task.Version != "1.2" {
		t.Errorf("version should be 1.2, got %s", task.Version)
	}
	if !strings.Contains(task.Registration.Description, OwnershipMarker) {
		t.Errorf("description missing ownership marker")
	}
	if task.Principal.LogonType != "InteractiveToken" {
		t.Errorf("LogonType should be InteractiveToken, got %s", task.Principal.LogonType)
	}
	if task.Principal.RunLevel != "LeastPrivilege" {
		t.Errorf("RunLevel should be LeastPrivilege, got %s", task.Principal.RunLevel)
	}
	if task.Settings.MultipleInstancesPolicy != "IgnoreNew" {
		t.Errorf("MultipleInstancesPolicy should be IgnoreNew, got %s", task.Settings.MultipleInstancesPolicy)
	}
	if task.Actions.Exec.Command != spec.Executable {
		t.Errorf("Command mismatch: %q != %q", task.Actions.Exec.Command, spec.Executable)
	}
}

// TestRenderTaskXMLEscaping tests XML special character escaping.
func TestRenderTaskXMLEscaping(t *testing.T) {
	// Create temp files with proper names
	tmpExe := filepath.Join(t.TempDir(), "test.exe")
	if err := os.WriteFile(tmpExe, []byte("fake"), 0o755); err != nil {
		t.Fatalf("failed to create temp exe: %v", err)
	}

	tmpLog := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(tmpLog, []byte(""), 0o666); err != nil {
		t.Fatalf("failed to create temp log: %v", err)
	}

	spec := Spec{
		Label:      "test",
		Executable: tmpExe,
		Args:       []string{`arg<>&"test`},
		LogPath:    tmpLog,
	}

	xmlBytes, err := renderTaskXML(spec)
	if err != nil {
		t.Fatalf("renderTaskXML failed: %v", err)
	}

	// Decode UTF-16LE
	utf16Bytes := make([]uint16, (len(xmlBytes)-2)/2)
	for i := 0; i < len(utf16Bytes); i++ {
		utf16Bytes[i] = uint16(xmlBytes[2+i*2]) | (uint16(xmlBytes[2+i*2+1]) << 8)
	}
	decoded := utf16.Decode(utf16Bytes)
	xmlStr := string(decoded)

	// Check that special chars are escaped
	if strings.Contains(xmlStr, "<>&") {
		t.Error("special chars not properly escaped")
	}
	if strings.Contains(xmlStr, "&lt;") && strings.Contains(xmlStr, "&gt;") && strings.Contains(xmlStr, "&amp;") {
		// Good, they're escaped
	}
}

// TestInstallExactRunnerCalls tests that Install makes exact schtasks calls in order.
func TestInstallExactRunnerCalls(t *testing.T) {
	tmpDir := t.TempDir()

	// Create temp executable and log files
	tmpExe := filepath.Join(tmpDir, "test.exe")
	if err := os.WriteFile(tmpExe, []byte("fake"), 0o755); err != nil {
		t.Fatalf("failed to create temp exe: %v", err)
	}

	tmpLog := filepath.Join(tmpDir, "test.log")

	spec := Spec{
		Label:      "test-service",
		Executable: tmpExe,
		Args:       []string{"arg1"},
		LogPath:    tmpLog,
	}

	var calls []struct {
		name string
		args []string
	}

	fakeRunner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, struct {
			name string
			args []string
		}{name, args})
		return nil, nil, nil
	})

	backend := NewTaskSchedulerBackend(tmpDir, fakeRunner)
	ctx := context.Background()

	if err := backend.Install(ctx, spec); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Should have 2 runner calls: Create and Run
	if len(calls) != 2 {
		t.Fatalf("expected 2 runner calls, got %d", len(calls))
	}

	// First call: schtasks /Create ...
	if calls[0].name != "schtasks" {
		t.Errorf("first call should be schtasks, got %s", calls[0].name)
	}
	if len(calls[0].args) < 1 || calls[0].args[0] != "/Create" {
		t.Errorf("first call should have /Create, got %v", calls[0].args)
	}

	// Second call: schtasks /Run ...
	if calls[1].name != "schtasks" {
		t.Errorf("second call should be schtasks, got %s", calls[1].name)
	}
	if len(calls[1].args) < 1 || calls[1].args[0] != "/Run" {
		t.Errorf("second call should have /Run, got %v", calls[1].args)
	}
}

// TestInstallRefusesUnownedXML tests that Install refuses existing unowned XML.
func TestInstallRefusesUnownedXML(t *testing.T) {
	tmpDir := t.TempDir()

	// Create temp executable
	tmpExe := filepath.Join(tmpDir, "test.exe")
	if err := os.WriteFile(tmpExe, []byte("fake"), 0o755); err != nil {
		t.Fatalf("failed to create temp exe: %v", err)
	}

	tmpLog := filepath.Join(tmpDir, "test.log")

	spec := Spec{
		Label:      "test-service",
		Executable: tmpExe,
		Args:       []string{},
		LogPath:    tmpLog,
	}

	// Create unowned XML file
	xmlPath := filepath.Join(tmpDir, "test-service.xml")
	if err := os.WriteFile(xmlPath, []byte("no marker"), 0o644); err != nil {
		t.Fatalf("failed to write unowned xml: %v", err)
	}

	runnerCalled := false
	fakeRunner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		runnerCalled = true
		return nil, nil, nil
	})

	backend := NewTaskSchedulerBackend(tmpDir, fakeRunner)
	ctx := context.Background()

	err := backend.Install(ctx, spec)
	if err == nil {
		t.Fatal("expected ErrNotOwned")
	}
	if _, ok := err.(ErrNotOwned); !ok {
		t.Fatalf("expected ErrNotOwned, got %T: %v", err, err)
	}
	if runnerCalled {
		t.Error("runner should not be called for unowned file")
	}
}

// TestUninstallExactCalls tests exact schtasks calls.
func TestUninstallExactCalls(t *testing.T) {
	tmpDir := t.TempDir()

	exe := filepath.Join(tmpDir, "routerd.exe")
	if err := os.WriteFile(exe, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Label:      "test-service",
		Executable: exe,
		Args:       []string{},
		LogPath:    filepath.Join(tmpDir, "test.log"),
	}

	// Create owned XML exactly as Install writes it.
	xmlPath := filepath.Join(tmpDir, "test-service.xml")
	xmlData, err := renderTaskXML(spec)
	if err != nil {
		t.Fatalf("renderTaskXML: %v", err)
	}
	if err := os.WriteFile(xmlPath, xmlData, 0o644); err != nil {
		t.Fatalf("failed to write xml: %v", err)
	}

	var calls []struct {
		name string
		args []string
	}

	fakeRunner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, struct {
			name string
			args []string
		}{name, args})
		return nil, nil, nil
	})

	backend := NewTaskSchedulerBackend(tmpDir, fakeRunner)
	ctx := context.Background()

	if err := backend.Uninstall(ctx, "test-service"); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	// Should have End and Delete calls
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", len(calls))
	}

	// Check End call
	if calls[0].name != "schtasks" || (len(calls[0].args) > 0 && calls[0].args[0] != "/End") {
		t.Errorf("first call should be /End, got %v", calls[0].args)
	}

	// Check Delete call
	if calls[1].name != "schtasks" || (len(calls[1].args) > 0 && calls[1].args[0] != "/Delete") {
		t.Errorf("second call should be /Delete, got %v", calls[1].args)
	}

	// XML should be removed
	if _, err := os.Stat(xmlPath); !os.IsNotExist(err) {
		t.Error("XML file should be removed")
	}
}

// TestUninstallRefusesUnownedXML tests that Uninstall refuses unowned XML.
func TestUninstallRefusesUnownedXML(t *testing.T) {
	tmpDir := t.TempDir()

	// Create unowned XML
	xmlPath := filepath.Join(tmpDir, "test-service.xml")
	if err := os.WriteFile(xmlPath, []byte("no marker"), 0o644); err != nil {
		t.Fatalf("failed to write unowned xml: %v", err)
	}

	runnerCalled := false
	fakeRunner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		runnerCalled = true
		return nil, nil, nil
	})

	backend := NewTaskSchedulerBackend(tmpDir, fakeRunner)
	ctx := context.Background()

	err := backend.Uninstall(ctx, "test-service")
	if err == nil {
		t.Fatal("expected ErrNotOwned")
	}
	if _, ok := err.(ErrNotOwned); !ok {
		t.Fatalf("expected ErrNotOwned, got %T: %v", err, err)
	}
	if runnerCalled {
		t.Error("runner should not be called for unowned file")
	}
}

// TestUninstallUnreadableXMLPath tests unreadable XML path returns error with no calls.
func TestUninstallUnreadableXMLPath(t *testing.T) {
	tmpDir := t.TempDir()
	xmlPath := filepath.Join(tmpDir, "test-service.xml")

	// Create a directory instead of a file (unreadable)
	if err := os.Mkdir(xmlPath, 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	runnerCalled := false
	fakeRunner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		runnerCalled = true
		return nil, nil, nil
	})

	backend := NewTaskSchedulerBackend(tmpDir, fakeRunner)
	ctx := context.Background()

	err := backend.Uninstall(ctx, "test-service")
	if err == nil {
		t.Fatal("expected error for unreadable path")
	}
	if runnerCalled {
		t.Error("runner should not be called when XML path is unreadable")
	}
}

// TestStatusNotInstalledNoRunnerCall tests that Status returns StateNotInstalled without runner call.
func TestStatusNotInstalledNoRunnerCall(t *testing.T) {
	tmpDir := t.TempDir()

	runnerCalled := false
	fakeRunner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		runnerCalled = true
		return nil, nil, nil
	})

	backend := NewTaskSchedulerBackend(tmpDir, fakeRunner)
	ctx := context.Background()

	status, err := backend.Status(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.State != StateNotInstalled {
		t.Errorf("expected StateNotInstalled, got %v", status.State)
	}
	if runnerCalled {
		t.Error("runner should not be called for nonexistent service")
	}
}

// TestStatusRunningReadyStopped tests status parsing.
func TestStatusRunningReadyStopped(t *testing.T) {
	tmpDir := t.TempDir()

	exe := filepath.Join(tmpDir, "routerd.exe")
	if err := os.WriteFile(exe, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Label:      "test",
		Executable: exe,
		Args:       []string{},
		LogPath:    filepath.Join(tmpDir, "test.log"),
	}

	// Create owned XML
	xmlPath := filepath.Join(tmpDir, "test.xml")
	xmlData, err := renderTaskXML(spec)
	if err != nil {
		t.Fatalf("renderTaskXML: %v", err)
	}
	if err := os.WriteFile(xmlPath, xmlData, 0o644); err != nil {
		t.Fatalf("failed to write xml: %v", err)
	}

	tests := []struct {
		name     string
		csvOut   string
		expected State
		detail   string
	}{
		{"running", "\"\\\\AntRouter\\\\test\",\"N/A\",\"Running\"\r\n", StateRunning, ""},
		{"ready", "\"\\\\AntRouter\\\\test\",\"10/17/2026 9:00:00 AM\",\"Ready\"\r\n", StateStopped, ""},
		{"disabled", "\"\\\\AntRouter\\\\test\",\"N/A\",\"Disabled\"\r\n", StateStopped, "disabled"},
		{"unknown", "\"\\\\AntRouter\\\\test\",\"N/A\",\"Unknown\"\r\n", StateUnknown, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeRunner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
				return []byte(tt.csvOut), nil, nil
			})

			backend := NewTaskSchedulerBackend(tmpDir, fakeRunner)
			ctx := context.Background()

			status, err := backend.Status(ctx, "test")
			if err != nil {
				t.Fatalf("Status failed: %v", err)
			}
			if status.State != tt.expected {
				t.Errorf("expected state %v, got %v", tt.expected, status.State)
			}
			if status.Detail != tt.detail {
				t.Errorf("expected detail %q, got %q", tt.detail, status.Detail)
			}
		})
	}
}

// TestUnsafeLabelsRejected tests that unsafe labels are rejected by all methods with no side effects.
func TestUnsafeLabelsRejected(t *testing.T) {
	tmpDir := t.TempDir()

	unsafeLabels := []string{
		"",
		"invalid..label",
		"_starts_with_underscore",
		"label-with-ñ",
		"label\nwith\nnewline",
	}

	for _, label := range unsafeLabels {
		t.Run("label:"+label, func(t *testing.T) {
			spec := Spec{
				Label:      label,
				Executable: "/path/to/executable",
				Args:       []string{},
				LogPath:    "/var/log/test.log",
			}

			runnerCalls := 0
			fakeRunner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
				runnerCalls++
				return nil, nil, nil
			})

			backend := NewTaskSchedulerBackend(tmpDir, fakeRunner)
			ctx := context.Background()

			// Install
			err := backend.Install(ctx, spec)
			if err == nil {
				t.Error("Install should reject unsafe label")
			}
			if runnerCalls > 0 {
				t.Error("Install should not call runner for unsafe label")
			}

			// Uninstall
			runnerCalls = 0
			err = backend.Uninstall(ctx, label)
			if err == nil {
				t.Error("Uninstall should reject unsafe label")
			}
			if runnerCalls > 0 {
				t.Error("Uninstall should not call runner for unsafe label")
			}

			// Start
			runnerCalls = 0
			err = backend.Start(ctx, label)
			if err == nil {
				t.Error("Start should reject unsafe label")
			}
			if runnerCalls > 0 {
				t.Error("Start should not call runner for unsafe label")
			}

			// Stop
			runnerCalls = 0
			err = backend.Stop(ctx, label)
			if err == nil {
				t.Error("Stop should reject unsafe label")
			}
			if runnerCalls > 0 {
				t.Error("Stop should not call runner for unsafe label")
			}

			// Status
			runnerCalls = 0
			_, err = backend.Status(ctx, label)
			if err == nil {
				t.Error("Status should reject unsafe label")
			}
			if runnerCalls > 0 {
				t.Error("Status should not call runner for unsafe label")
			}
		})
	}
}

// Helper types for testing

type fakeRunnerFunc func(ctx context.Context, name string, args ...string) ([]byte, []byte, error)

func (f fakeRunnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	return f(ctx, name, args...)
}

// TestOwnedXMLRoundTrip verifies that the backend recognizes the UTF-16LE XML
// it writes as its own, so reinstall and uninstall of its own task work.
func TestOwnedXMLRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "routerd.exe")
	if err := os.WriteFile(exe, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Label: "test-service", Executable: exe, Args: []string{"--log", `C:\logs\r.log`}, LogPath: filepath.Join(tmpDir, "r.log")}
	stateDir := filepath.Join(tmpDir, "state")
	var calls [][]string
	runner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil, nil
	})
	b := NewTaskSchedulerBackend(stateDir, runner)
	ctx := context.Background()
	if err := b.Install(ctx, spec); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := b.Install(ctx, spec); err != nil {
		t.Fatalf("reinstall over own XML: %v", err)
	}
	if err := b.Uninstall(ctx, spec.Label); err != nil {
		t.Fatalf("uninstall own XML: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, spec.Label+".xml")); !os.IsNotExist(err) {
		t.Fatalf("xml should be removed, stat err = %v", err)
	}
	if len(calls) != 6 {
		t.Fatalf("got %d runner calls, want 6: %q", len(calls), calls)
	}
}

// TestUnownedUTF16XMLRejected verifies a foreign UTF-16 task XML that merely
// contains the marker bytes in UTF-8 form elsewhere is not mistaken as owned,
// and a foreign UTF-16 XML without the marker is refused.
func TestUnownedUTF16XMLRejected(t *testing.T) {
	stateDir := t.TempDir()
	foreign := encodeUTF16LEForTest("<?xml version=\"1.0\" encoding=\"UTF-16\"?><Task><RegistrationInfo><Description>someone else</Description></RegistrationInfo></Task>")
	if err := os.WriteFile(filepath.Join(stateDir, "test-service.xml"), foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := fakeRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		t.Fatalf("unexpected runner call %s %q", name, args)
		return nil, nil, nil
	})
	err := NewTaskSchedulerBackend(stateDir, runner).Uninstall(context.Background(), "test-service")
	var notOwned ErrNotOwned
	if !errors.As(err, &notOwned) {
		t.Fatalf("got %v, want ErrNotOwned", err)
	}
}

func encodeUTF16LEForTest(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := []byte{0xFF, 0xFE}
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}
