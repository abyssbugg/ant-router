package daemon

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/abyssbugg/ant-router/internal/app"
	"github.com/abyssbugg/ant-router/internal/buildinfo"
	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/configservice"
	"github.com/abyssbugg/ant-router/internal/configstore"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/dataplane"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
)

type Options struct {
	Instance        string
	StateRoot       string
	RuntimeRoot     string
	Build           buildinfo.Info
	ShutdownTimeout time.Duration
	DataPlaneListen func(string) (net.Listener, error)
}

type Daemon struct {
	paths           runtimepaths.Paths
	endpoint        controltransport.Endpoint
	token           controltoken.Token
	lifecycle       *app.Lifecycle
	configService   *configservice.Service
	server          *control.Server
	dataPlane       *activeDataPlane
	startedAt       time.Time
	shutdownTimeout time.Duration
	ready           chan struct{}
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
	dataPlaneListen := options.DataPlaneListen
	if dataPlaneListen == nil {
		dataPlaneListen = dataplane.ListenLoopback
	}
	dataPlane, err := buildDataPlane(paths, manager, dataPlaneListen)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	shutdownTimeout := options.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}
	if dataPlane != nil && dataPlane.shutdownTimeout > shutdownTimeout {
		shutdownTimeout = dataPlane.shutdownTimeout
	}
	daemon := &Daemon{
		paths:           paths,
		endpoint:        endpoint,
		token:           token,
		lifecycle:       app.NewLifecycle(),
		configService:   service,
		dataPlane:       dataPlane,
		startedAt:       time.Now().UTC(),
		shutdownTimeout: shutdownTimeout,
		ready:           make(chan struct{}),
	}
	serverOptions := control.DefaultServerOptions()
	serverOptions.Build = options.Build
	serverOptions.Config = service
	serverOptions.ShutdownTimeout = shutdownTimeout
	server, err := control.NewServer(listener, token, daemon, serverOptions)
	if err != nil {
		_ = listener.Close()
		dataPlane.closeBeforeStart()
		return nil, err
	}
	daemon.server = server
	return daemon, nil
}

func (d *Daemon) Ready() <-chan struct{} { return d.ready }

func (d *Daemon) Run(ctx context.Context) error {
	serverCount := 1
	if d.dataPlane != nil {
		if err := d.dataPlane.server.Start(); err != nil {
			d.lifecycle.Stop()
			return err
		}
		serverCount++
	}
	if err := d.server.Start(); err != nil {
		d.lifecycle.BeginDrain()
		shutdownContext, cancel := context.WithTimeout(context.Background(), d.shutdownTimeout)
		shutdownErr := d.shutdownDataPlane(shutdownContext)
		cancel()
		d.lifecycle.Stop()
		return errors.Join(err, shutdownErr)
	}

	if d.dataPlane != nil {
		<-d.dataPlane.server.Ready()
	}
	<-d.server.Ready()
	close(d.ready)

	serveResults := make(chan error, serverCount)
	go func() { serveResults <- d.server.Wait() }()
	if d.dataPlane != nil {
		go func() { serveResults <- d.dataPlane.server.Wait() }()
	}

	var trigger error
	select {
	case trigger = <-serveResults:
		if trigger == nil {
			trigger = errors.New("daemon server stopped unexpectedly")
		}
		serverCount--
	case <-ctx.Done():
	}

	d.lifecycle.BeginDrain()
	shutdownContext, cancel := context.WithTimeout(context.Background(), d.shutdownTimeout)
	shutdownErr := errors.Join(d.shutdownDataPlane(shutdownContext), d.server.Shutdown(shutdownContext))
	cancel()
	for range serverCount {
		shutdownErr = errors.Join(shutdownErr, <-serveResults)
	}
	d.lifecycle.Stop()
	return errors.Join(trigger, shutdownErr)
}

func (d *Daemon) shutdownDataPlane(ctx context.Context) error {
	if d.dataPlane == nil {
		return nil
	}
	return d.dataPlane.server.Shutdown(ctx)
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
	var dataPlane *control.DataPlaneSummary
	if d.dataPlane != nil {
		copyValue := d.dataPlane.status
		dataPlane = &copyValue
	}
	return control.RuntimeStatus{Instance: d.paths.Instance.String(), DaemonState: state, StartedAt: d.startedAt, Config: summary, DataPlane: dataPlane}, nil
}
