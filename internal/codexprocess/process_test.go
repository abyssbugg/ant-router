package codexprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCodexProcessHelper(t *testing.T) {
	if os.Getenv("ANT_ROUTER_CODEX_HELPER") != "1" {
		return
	}
	args := os.Args
	marker := 0
	for index, arg := range args {
		if arg == "--" {
			marker = index + 1
			break
		}
	}
	args = args[marker:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Print("codex-cli 0.153.4\n")
		os.Exit(0)
	}
	if len(args) > 0 && args[len(args)-1] == "stdin" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1024))
		if err != nil {
			os.Exit(90)
		}
		fmt.Printf("STDIN=%s", data)
		os.Exit(0)
	}
	if len(args) > 0 && args[len(args)-1] == "exit7" {
		os.Exit(7)
	}
	if len(args) > 0 && args[len(args)-1] == "sleep" {
		time.Sleep(30 * time.Second)
	}
	if len(args) > 0 && args[len(args)-1] == "child-sleep" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if len(args) >= 2 && args[len(args)-2] == "spawn-child" {
		executable, err := os.Executable()
		if err != nil {
			os.Exit(91)
		}
		child := exec.Command(executable, "-test.run=TestCodexProcessHelper", "--", "child-sleep")
		child.Env = append(os.Environ(), "ANT_ROUTER_CODEX_HELPER=1")
		if err := child.Start(); err != nil {
			os.Exit(91)
		}
		pidPath := args[len(args)-1]
		temporaryPath := pidPath + ".tmp"
		if err := os.WriteFile(temporaryPath, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			_ = child.Process.Kill()
			os.Exit(92)
		}
		if err := os.Rename(temporaryPath, pidPath); err != nil {
			_ = os.Remove(temporaryPath)
			_ = child.Process.Kill()
			os.Exit(92)
		}
		_ = child.Wait()
		os.Exit(0)
	}
	fmt.Printf("HOME=%s ARGS=%s", os.Getenv("CODEX_HOME"), strings.Join(args, "|"))
	os.Exit(0)
}

func helperRunner(t *testing.T) *Runner {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(executable, []string{"-test.run=TestCodexProcessHelper", "--"}, []string{"ANT_ROUTER_CODEX_HELPER=1"})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestRunnerVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	version, err := helperRunner(t).Version(ctx, false)
	if err != nil || version.String() != "0.153.4" {
		t.Fatalf("version = %v, %v", version, err)
	}
}

func TestRunnerUsesDirectProfileArgumentsAndIsolatedHome(t *testing.T) {
	var stdout, stderr bytes.Buffer
	result, err := helperRunner(t).Run(context.Background(), RunOptions{CodexHome: "/private/codex", Profile: "ant-router", Args: []string{"exec", "hello;whoami"}, Stdout: &stdout, Stderr: &stderr})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("run = %#v, %v", result, err)
	}
	want := "HOME=/private/codex ARGS=--profile|ant-router|exec|hello;whoami"
	if stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunnerCancellationStopsCodex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := helperRunner(t).Run(ctx, RunOptions{CodexHome: "/private/codex", Profile: "ant-router", Args: []string{"sleep"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerRejectsInvalidRunOptions(t *testing.T) {
	for _, options := range []RunOptions{{CodexHome: "relative", Profile: "ant-router"}, {CodexHome: "/private/codex", Profile: "../bad"}, {CodexHome: "/private/codex", Profile: ""}} {
		if _, err := helperRunner(t).Run(context.Background(), options); err == nil {
			t.Fatalf("accepted %#v", options)
		}
	}
}

func TestRunnerPreservesStdin(t *testing.T) {
	var stdout bytes.Buffer
	result, err := helperRunner(t).Run(context.Background(), RunOptions{CodexHome: "/private/codex", Profile: "ant-router", Args: []string{"stdin"}, Stdin: strings.NewReader("hello"), Stdout: &stdout})
	if err != nil || result.ExitCode != 0 || stdout.String() != "STDIN=hello" {
		t.Fatalf("run=%#v output=%q error=%v", result, stdout.String(), err)
	}
}

func TestRunnerReturnsCodexExitCode(t *testing.T) {
	result, err := helperRunner(t).Run(context.Background(), RunOptions{CodexHome: "/private/codex", Profile: "ant-router", Args: []string{"exit7"}})
	if err != nil || result.ExitCode != 7 {
		t.Fatalf("run = %#v, %v", result, err)
	}
}
