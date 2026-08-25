package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
	"github.com/abyssbugg/ant-router/internal/daemon"
)

type daemonRunner interface {
	Run(context.Context) error
}

type daemonFactory func(daemon.Options) (daemonRunner, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	factory := func(options daemon.Options) (daemonRunner, error) { return daemon.New(options) }
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, factory))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, factory daemonFactory) int {
	_ = stdout
	flags := flag.NewFlagSet("routerd", flag.ContinueOnError)
	flags.SetOutput(stderr)
	instance := flags.String("instance", "", "named daemon instance")
	stateRoot := flags.String("state-dir", "", "persistent state root")
	runtimeRoot := flags.String("runtime-dir", "", "local runtime root")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return 2
	}
	options := daemon.Options{
		Instance:    *instance,
		StateRoot:   *stateRoot,
		RuntimeRoot: *runtimeRoot,
		Build:       buildinfo.Current(),
	}
	runner, err := factory(options)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "ant-router: startup failed: %v\n", err)
		return 1
	}
	if err := runner.Run(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "ant-router: daemon failed: %v\n", err)
		return 1
	}
	return 0
}
