package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/abyssbugg/ant-router/internal/app"
	"github.com/abyssbugg/ant-router/internal/buildinfo"
	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/configservice"
	"github.com/abyssbugg/ant-router/internal/configstore"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
)

type Options struct {
	Instance        string
	StateRoot       string
	RuntimeRoot     string
	Build           buildinfo.Info
	ShutdownTimeout time.Duration
}

type Daemon struct {
	paths           runtimepaths.Paths
	endpoint        controltransport.Endpoint
	token           controltoken.Token
	lifecycle       *app.Lifecycle
	configService   *configservice.Service
	server          *control.Server
	startedAt       time.Time
	shutdownTimeout time.Duration
}

func New(options Options) (*Daemon, error) {
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err != nil {
		return nil, err
	}
	tokenStore, err := controltoken.NewStore(paths.StateRoot)
	if err != nil {
		return nil, err
	}
	token, err := tokenStore.LoadOrCreate(paths.Instance)
	if err != nil {
		return nil, err
	}
	store, err := configstore.NewFileStore(paths.ConfigRoot)
	if err != nil {
		return nil, err
	}
	manager, err := config.NewManager(store, time.Now)
	if err != nil {
		return nil, err
	}
	service, err := configservice.New(manager, store)
	if err != nil {
		return nil, err
	}
	state, initializeErr := service.Initialize(context.Background())
	if initializeErr != nil && state != configservice.StateDegraded {
		return nil, initializeErr
	}
	endpoint, err := controltransport.ResolveLocalEndpoint(paths.Instance, controltransport.ResolveOptions{StateRoot: paths.StateRoot, RuntimeRoot: paths.RuntimeRoot})
	if err != nil {
		return nil, err
	}
	listener, err := controltransport.ListenLocal(endpoint)
	if err != nil {
		return nil, err
	}
	shutdownTimeout := options.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}
	daemon := &Daemon{
		paths:           paths,
		endpoint:        endpoint,
		token:           token,
		lifecycle:       app.NewLifecycle(),
		configService:   service,
		startedAt:       time.Now().UTC(),
		shutdownTimeout: shutdownTimeout,
	}
	serverOptions := control.DefaultServerOptions()
	serverOptions.Build = options.Build
	serverOptions.Config = service
	serverOptions.ShutdownTimeout = shutdownTimeout
	server, err := control.NewServer(listener, token, daemon, serverOptions)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	daemon.server = server
	return daemon, nil
}

func (d *Daemon) Ready() <-chan struct{} { return d.server.Ready() }

func (d *Daemon) Run(ctx context.Context) error {
	if err := d.server.Start(); err != nil {
		d.lifecycle.Stop()
		return err
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- d.server.Wait() }()
	select {
	case err := <-serveResult:
		d.lifecycle.Stop()
		return err
	case <-ctx.Done():
		d.lifecycle.BeginDrain()
		shutdownContext, cancel := context.WithTimeout(context.Background(), d.shutdownTimeout)
		shutdownErr := d.server.Shutdown(shutdownContext)
		cancel()
		serveErr := <-serveResult
		d.lifecycle.Stop()
		return errors.Join(shutdownErr, serveErr)
	}
}

func (d *Daemon) Status(ctx context.Context) (control.RuntimeStatus, error) {
	if err := ctx.Err(); err != nil {
		return control.RuntimeStatus{}, err
	}
	state := "running"
	switch d.lifecycle.State() {
	case app.StateDraining:
		state = "draining"
	case app.StateStopped:
		state = "stopped"
	}
	current := d.configService.Current()
	var summary *control.ConfigSummary
	if current.State == configservice.StateRunning {
		summary = &control.ConfigSummary{Revision: current.Revision.String(), Generation: current.Generation, Status: string(current.Status)}
	}
	return control.RuntimeStatus{Instance: d.paths.Instance.String(), DaemonState: state, StartedAt: d.startedAt, Config: summary}, nil
}
