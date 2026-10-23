package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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
	logFile := flags.String("log-file", "", "append diagnostics to this absolute path instead of stderr")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *logFile != "" && !filepath.IsAbs(*logFile) {
		flags.Usage()
		return 2
	}
	if *logFile != "" {
		// Service managers that cannot redirect output (Windows Task
		// Scheduler) pass the log path here; the file is opened for append
		// and owner-only on create, its directory created if missing (a
		// fresh install has none yet).
		if err := os.MkdirAll(filepath.Dir(*logFile), 0o700); err != nil {
			_, _ = fmt.Fprintf(stderr, "ant-router: create log directory: %v\n", err)
			return 1
		}
		file, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "ant-router: open log file: %v\n", err)
			return 1
		}
		defer file.Close()
		stdout, stderr = file, file
		// http.Server and other standard-logger users write to the process
		// logger when no ErrorLog is set; send them to the log file too.
		previous := log.Writer()
		log.SetOutput(file)
		defer log.SetOutput(previous)
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
