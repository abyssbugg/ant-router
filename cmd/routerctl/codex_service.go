package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/abyssbugg/ant-router/internal/codexintegration"
	"github.com/abyssbugg/ant-router/internal/codexownership"
	"github.com/abyssbugg/ant-router/internal/codexprocess"
	"github.com/abyssbugg/ant-router/internal/codexprofile"
	"github.com/abyssbugg/ant-router/internal/codexversion"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
)

type codexService interface {
	Plan(context.Context, codexintegration.Action, string) (codexintegration.Plan, error)
	Apply(context.Context, codexintegration.Action, string, string) (codexintegration.Result, error)
	Token(context.Context) (string, error)
	Run(context.Context, []string, io.Reader, io.Writer, io.Writer) (int, error)
	Status(context.Context) (codexServiceStatus, error)
	Doctor(context.Context) (codexServiceStatus, error)
}

type codexServiceStatus struct {
	State, ProfilePath, CatalogPath, CodexVersion string
	Changes                                       int
}

type localCodexService struct {
	options                            cliOptions
	client                             controlClient
	paths                              runtimepaths.Paths
	codexHome, codexBin, routerctlPath string
	store                              *codexownership.Store
	runner                             *codexprocess.Runner
}

type unavailableCatalogSource struct{}

func (unavailableCatalogSource) CodexCatalog(context.Context) (control.CodexCatalogResponse, error) {
	return control.CodexCatalogResponse{}, codexintegration.ErrUnavailable
}

type unavailableVersionSource struct{}

func (unavailableVersionSource) Version(context.Context, bool) (codexversion.Version, error) {
	return codexversion.Version{}, codexintegration.ErrUnavailable
}

func newCodexService(options cliOptions, client controlClient) (*localCodexService, error) {
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err != nil {
		return nil, err
	}
	codexHome := options.CodexHome
	if codexHome == "" {
		codexHome = os.Getenv("CODEX_HOME")
	}
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.Join(codexintegration.ErrUnavailable, errors.New("Codex home unavailable"))
		}
		codexHome = filepath.Join(home, ".codex")
	}
	codexHome, err = filepath.Abs(codexHome)
	if err != nil {
		return nil, errors.Join(codexintegration.ErrUnavailable, errors.New("Codex home unavailable"))
	}
	routerctlPath, err := os.Executable()
	if err != nil {
		return nil, errors.New("routerctl executable unavailable")
	}
	routerctlPath, err = filepath.Abs(routerctlPath)
	if err != nil {
		return nil, errors.New("routerctl executable unavailable")
	}
	store, err := codexownership.NewStore(paths.StateRoot)
	if err != nil {
		return nil, err
	}
	return &localCodexService{options: options, client: client, paths: paths, codexHome: filepath.Clean(codexHome), routerctlPath: routerctlPath, store: store}, nil
}

func (service *localCodexService) runnerInstance() (*codexprocess.Runner, error) {
	if service.runner != nil {
		return service.runner, nil
	}
	codexBin := service.options.CodexBin
	var err error
	if codexBin == "" {
		codexBin, err = exec.LookPath("codex")
		if err != nil {
			return nil, errors.Join(codexintegration.ErrUnavailable, errors.New("Codex executable unavailable"))
		}
	}
	codexBin, err = filepath.Abs(codexBin)
	if err != nil {
		return nil, errors.Join(codexintegration.ErrUnavailable, errors.New("Codex executable unavailable"))
	}
	runner, err := codexprocess.New(codexBin, nil, nil)
	if err != nil {
		return nil, err
	}
	service.runner = runner
	return runner, nil
}

func (service *localCodexService) planner(ctx context.Context, action codexintegration.Action) (*codexintegration.Planner, error) {
	baseURL := "http://127.0.0.1:1/v1"
	var catalog codexintegration.CatalogSource = unavailableCatalogSource{}
	var versions codexintegration.VersionSource = unavailableVersionSource{}
	if action == codexintegration.ActionEnable {
		if service.client == nil {
			return nil, codexintegration.ErrUnavailable
		}
		status, err := service.client.Status(ctx)
		if err != nil {
			return nil, err
		}
		if status.DataPlane == nil || !status.DataPlane.Enabled {
			return nil, errDataPlaneUnavailable
		}
		baseURL = status.DataPlane.BaseURL
		runner, err := service.runnerInstance()
		if err != nil {
			return nil, err
		}
		catalog, versions = service.client, runner
	}
	return codexintegration.NewPlanner(codexintegration.PlannerOptions{CodexHome: service.codexHome, StateRoot: service.paths.StateRoot, RouterctlPath: service.routerctlPath, BaseURL: baseURL, Instance: service.paths.Instance.String(), RuntimeRoot: service.paths.RuntimeRoot, AllowUntested: service.options.AllowUntestedCodex}, catalog, versions, service.store)
}

func (service *localCodexService) Plan(ctx context.Context, action codexintegration.Action, generation string) (codexintegration.Plan, error) {
	planner, err := service.planner(ctx, action)
	if err != nil {
		return codexintegration.Plan{}, err
	}
	switch action {
	case codexintegration.ActionEnable:
		return planner.PlanEnable(ctx)
	case codexintegration.ActionDisable:
		return planner.PlanDisable(ctx)
	case codexintegration.ActionRestore:
		return planner.PlanRestore(ctx, generation)
	default:
		return codexintegration.Plan{}, errUsage
	}
}

func (service *localCodexService) Apply(ctx context.Context, action codexintegration.Action, generation, proof string) (codexintegration.Result, error) {
	planner, err := service.planner(ctx, action)
	if err != nil {
		return codexintegration.Result{}, err
	}
	manager, err := codexintegration.NewManager(planner, service.store, time.Now)
	if err != nil {
		return codexintegration.Result{}, err
	}
	if err := manager.Recover(ctx); err != nil {
		return codexintegration.Result{}, err
	}
	switch action {
	case codexintegration.ActionEnable:
		return manager.Enable(ctx, proof)
	case codexintegration.ActionDisable:
		return manager.Disable(ctx, proof)
	case codexintegration.ActionRestore:
		return manager.Restore(ctx, generation, proof)
	default:
		return codexintegration.Result{}, errUsage
	}
}

func (service *localCodexService) Token(ctx context.Context) (string, error) {
	token, err := service.client.InferenceToken(ctx)
	if err != nil {
		return "", err
	}
	return token.String(), nil
}
func (service *localCodexService) Run(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	runner, err := service.runnerInstance()
	if err != nil {
		return 0, err
	}
	result, err := runner.Run(ctx, codexprocess.RunOptions{CodexHome: service.codexHome, Profile: codexprofile.ProviderID, Args: arguments, Stdin: stdin, Stdout: stdout, Stderr: stderr})
	return result.ExitCode, err
}
func (service *localCodexService) Doctor(ctx context.Context) (codexServiceStatus, error) {
	status, err := service.Status(ctx)
	if err != nil {
		return codexServiceStatus{}, err
	}
	if service.client == nil {
		return status, codexintegration.ErrUnavailable
	}
	if _, err := service.client.InferenceToken(ctx); err != nil {
		return status, err
	}
	if status.Changes != 0 {
		return status, codexintegration.ErrConflict
	}
	return status, nil
}

func (service *localCodexService) Status(ctx context.Context) (codexServiceStatus, error) {
	plan, err := service.Plan(ctx, codexintegration.ActionEnable, "")
	if err != nil {
		return codexServiceStatus{}, err
	}
	state := "enabled"
	if len(plan.Changes()) != 0 {
		state = "disabled_or_outdated"
	}
	return codexServiceStatus{State: state, ProfilePath: plan.ProfilePath(), CatalogPath: plan.CatalogPath(), CodexVersion: plan.CodexVersion(), Changes: len(plan.Changes())}, nil
}
