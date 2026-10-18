package daemon

import (
	"fmt"
	"net"
	"time"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/dataplane"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
)

const dataPlaneRequestTimeout = 2 * time.Minute

type activeDataPlane struct {
	listener        net.Listener
	server          *dataplane.Server
	status          control.DataPlaneSummary
	shutdownTimeout time.Duration
}

func (d *activeDataPlane) closeBeforeStart() {
	if d != nil && d.listener != nil {
		_ = d.listener.Close()
	}
}

func buildDataPlane(paths runtimepaths.Paths, manager *config.Manager, providers *reloadableProviders, listen func(string) (net.Listener, error)) (*activeDataPlane, error) {
	snapshot, ok := manager.Current()
	if !ok {
		return nil, nil
	}
	configuration := snapshot.Config()
	if err := configuration.ValidateForInstance(paths.Instance.String()); err != nil {
		return nil, err
	}
	if !configuration.DataPlane.Enabled {
		return nil, nil
	}
	tokenStore, err := inferencetoken.NewStore(paths.StateRoot)
	if err != nil {
		return nil, err
	}
	if _, err := tokenStore.LoadOrCreate(paths.Instance); err != nil {
		return nil, err
	}
	if providers == nil || providers.load() == nil || providers.load().registry == nil {
		return nil, fmt.Errorf("provider registry is unavailable")
	}
	handler, err := dataplane.NewHandler(dataplane.HandlerOptions{
		TokenSource:      liveInferenceTokenSource{store: tokenStore, instance: paths.Instance},
		Executor:         providers,
		Catalog:          providers,
		AffinityResolver: providers,
		Runtime:          func() dataplane.RequestRuntime { return providers.load().registry },
		ListenAddress:    configuration.DataPlane.Listen,
		MaxConcurrent:    configuration.DataPlane.MaxConcurrentRequests,
		RequestTimeout:   dataPlaneRequestTimeout,
		Clock:            time.Now,
	})
	if err != nil {
		return nil, err
	}
	listener, err := listen(configuration.DataPlane.Listen)
	if err != nil {
		return nil, err
	}
	serverOptions := dataplane.DefaultServerOptions()
	serverOptions.ShutdownTimeout = configuration.DataPlane.ShutdownTimeout.Duration
	server, err := dataplane.NewServer(listener, handler, serverOptions)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	return &activeDataPlane{
		listener:        listener,
		server:          server,
		status:          control.DataPlaneSummary{Enabled: true, BaseURL: fmt.Sprintf("http://%s/v1", configuration.DataPlane.Listen)},
		shutdownTimeout: configuration.DataPlane.ShutdownTimeout.Duration,
	}, nil
}
