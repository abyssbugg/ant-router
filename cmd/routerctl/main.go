package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
)

type clientFactory func(cliOptions) (controlClient, error)

type staticTokenSource struct{ token controltoken.Token }

func (s staticTokenSource) Token(context.Context) (controltoken.Token, error) { return s.token, nil }

type localControlClient struct {
	*control.Client
	inferenceStore *inferencetoken.Store
	instance       controltransport.InstanceName
}

func (c *localControlClient) InferenceToken(ctx context.Context) (inferencetoken.Token, error) {
	if err := ctx.Err(); err != nil {
		return inferencetoken.Token{}, err
	}
	return c.inferenceStore.Load(c.instance)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, newControlClient))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, factory clientFactory) int {
	options, command, err := parseCLI(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitSuccess
		}
		_ = writeCLIError(stderr, options, errUsage)
		return ExitUsage
	}
	if command.Name == "version" {
		if err := writeVersion(stdout, options); err != nil {
			_ = writeCLIError(stderr, options, err)
			return ExitInternal
		}
		return ExitSuccess
	}
	client, err := factory(options)
	if err != nil {
		code := exitCode(err)
		if code == ExitInternal {
			err = errors.Join(errConnection, err)
			code = ExitConnection
		}
		_ = writeCLIError(stderr, options, err)
		return code
	}
	defer client.CloseIdleConnections()
	return execute(ctx, options, command, stdin, stdout, stderr, client)
}

func newControlClient(options cliOptions) (controlClient, error) {
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err != nil {
		return nil, err
	}
	tokenStore, err := controltoken.NewStore(paths.StateRoot)
	if err != nil {
		return nil, err
	}
	token, err := tokenStore.Load(paths.Instance)
	if err != nil {
		return nil, err
	}
	endpoint, err := controltransport.ResolveLocalEndpoint(paths.Instance, controltransport.ResolveOptions{StateRoot: paths.StateRoot, RuntimeRoot: paths.RuntimeRoot})
	if err != nil {
		return nil, err
	}
	client, err := control.NewClient(controltransport.LocalDialer{Endpoint: endpoint}, staticTokenSource{token: token}, control.DefaultClientOptions())
	if err != nil {
		return nil, err
	}
	inferenceStore, err := inferencetoken.NewStore(paths.StateRoot)
	if err != nil {
		return nil, err
	}
	return &localControlClient{Client: client, inferenceStore: inferenceStore, instance: paths.Instance}, nil
}
