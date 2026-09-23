package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const maxResponseBytes = 1 << 20

type boundedBuffer struct{ data bytes.Buffer }

func (b *boundedBuffer) Len() int { return b.data.Len() }
func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > maxResponseBytes-b.data.Len() {
		return 0, errors.New("CircleCI output exceeds bound")
	}
	return b.data.Write(p)
}

func apiArgs(method, path string) ([]string, error) {
	parts := strings.Split(path, "/")
	allowed := false
	if len(parts) >= 4 && parts[0] == "api" && parts[1] == "v2" && uuidPattern.MatchString(parts[3]) {
		allowed = method == "GET" && (len(parts) == 4 && (parts[2] == "workflow" || parts[2] == "pipeline") || len(parts) == 5 && parts[2] == "workflow" && parts[4] == "job")
		allowed = allowed || method == "POST" && len(parts) == 5 && parts[2] == "workflow" && parts[4] == "cancel"
	}
	if !allowed {
		return nil, errors.New("API operation not permitted by watchdog")
	}
	return []string{"api", path, "--method", method, "--no-color"}, nil
}

type cliAPI struct{}

func (cliAPI) Call(ctx context.Context, method, path string) (json.RawMessage, error) {
	args, err := apiArgs(method, path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "circleci", args...)
	cmd.WaitDelay = time.Second
	var out boundedBuffer
	cmd.Stdout = &out
	// Do not echo CLI errors, auth URLs or credentials into logs.
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, errors.New("CircleCI CLI request failed or timed out")
	}
	return json.RawMessage(out.data.Bytes()), nil
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func run(ctx context.Context, args []string, client api, timer clock, out io.Writer) error {
	flags := flag.NewFlagSet("circleci-watchdog", flag.ContinueOnError)
	flags.SetOutput(out)
	var want target
	flags.StringVar(&want.Project, "project", "", "canonical project_slug returned by the pipeline API")
	flags.StringVar(&want.Pipeline, "pipeline", "", "explicit pipeline UUID")
	flags.StringVar(&want.Workflow, "workflow", "", "explicit pr-coverage workflow UUID")
	flags.StringVar(&want.Branch, "branch", "", "approved branch")
	flags.StringVar(&want.Revision, "revision", "", "approved full lowercase commit SHA")
	allowCancel := flags.Bool("allow-cancel", false, "explicitly authorize monitoring and one target cancellation; default is identity check only")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if err := want.validate(); err != nil {
		return err
	}
	if !*allowCancel {
		if _, err := identify(ctx, client, want); err != nil {
			return err
		}
		fmt.Fprintln(out, "Identity verified READ-ONLY; watchdog NOT ARMED; no coverage result claimed")
		return nil
	}
	fmt.Fprintf(out, "Monitoring explicit workflow %s; job deadline 15m; session deadline 45m; no launch/approval/rerun\n", want.Workflow)
	if err := monitor(ctx, client, timer, want); err != nil {
		return err
	}
	fmt.Fprintln(out, "Workflow and all four required jobs succeeded within their deadlines")
	return nil
}

// Run externally, not as a billable CircleCI job. Start before approving Linux;
// keep this host awake/connected. Default mode only verifies identity. A live
// --allow-cancel invocation needs fresh user authorization for the exact target.
// SIGINT/SIGTERM request cleanup only after identity verification; SIGKILL,
// host failure, suspended processes, clock errors and API outages cannot provide
// a guaranteed spending cap. Inspect every UNCONFIRMED or UNSUPERVISED result
// manually: the target may still be running. Never restart with a different
// target or issue another uncertain POST automatically.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], cliAPI{}, realClock{}, os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
