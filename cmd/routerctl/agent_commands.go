package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/abyssbugg/ant-router/internal/agentpublication"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
)

const geminiSettingsRelative = "settings.json"

// parseAgentCommand parses `agent <action> --agent <name>` with the actions
// publish, status, disable, and restore. Only gemini is supported in this
// increment; the grammar reserves the --agent flag for future targets.
func parseAgentCommand(args []string, command *cliCommand) error {
	if len(args) < 2 {
		return errUsage
	}
	command.Action = args[1]
	switch command.Action {
	case "publish", "disable", "restore", "status":
		agent := ""
		for index := 2; index < len(args); index++ {
			switch {
			case args[index] == "--agent" && index+1 < len(args):
				agent = args[index+1]
				index++
			default:
				return errUsage
			}
		}
		if agent != "gemini" {
			return errUsage
		}
		command.Agent = agent
	default:
		return errUsage
	}
	return nil
}

func geminiHome(options cliOptions) (string, error) {
	if options.GeminiHome != "" {
		return options.GeminiHome, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("Gemini home cannot be resolved; pass --gemini-home")
	}
	return filepath.Join(home, ".gemini"), nil
}

func resolveAgentContext(ctx context.Context, options cliOptions, client controlClient) (control.DataPlaneSummary, inferencetoken.Token, error) {
	status, err := client.Status(ctx)
	if err != nil {
		return control.DataPlaneSummary{}, inferencetoken.Token{}, err
	}
	if status.DataPlane == nil || !status.DataPlane.Enabled {
		return control.DataPlaneSummary{}, inferencetoken.Token{}, errDataPlaneUnavailable
	}
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err != nil {
		return control.DataPlaneSummary{}, inferencetoken.Token{}, err
	}
	store, err := inferencetoken.NewStore(paths.StateRoot)
	if err != nil {
		return control.DataPlaneSummary{}, inferencetoken.Token{}, err
	}
	token, err := store.Load(controltransport.InstanceName(status.Instance))
	if err != nil {
		return control.DataPlaneSummary{}, inferencetoken.Token{}, err
	}
	return *status.DataPlane, token, nil
}

func executeAgent(ctx context.Context, options cliOptions, command cliCommand, stdout, stderr io.Writer, factory clientFactory) int {
	if command.Action == "status" || command.Action == "disable" || command.Action == "restore" {
		return executeAgentOffline(ctx, options, command, stdout, stderr)
	}
	// publish needs the live daemon for the data plane base URL and the
	// inference token; disable and restore are handled offline above.
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
	home, err := geminiHome(options)
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	settingsPath := filepath.Join(home, geminiSettingsRelative)
	switch command.Action {
	case "publish":
		dataPlane, token, err := resolveAgentContext(ctx, options, client)
		if err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		if options.AgentModel == "" {
			err := fmt.Errorf("agent publication requires --agent-model <public alias>")
			_ = writeCLIError(stderr, options, err)
			return ExitUsage
		}
		// The daemon advertises BaseURL with the /v1 path already.
		baseURL := dataPlane.BaseURL
		input := agentpublication.PublicationInput{BaseURL: baseURL, APIKey: token.String(), Model: options.AgentModel}
		result, err := runAgentPublish(ctx, options, settingsPath, input)
		if err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		if err := writeAgentResult(stdout, options, result, "Restart Gemini CLI to load the published model provider."); err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		return ExitSuccess
	default:
		_ = writeCLIError(stderr, options, errUsage)
		return ExitUsage
	}
}

func runAgentPublish(ctx context.Context, options cliOptions, settingsPath string, input agentpublication.PublicationInput) (agentpublication.Result, error) {
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err != nil {
		return agentpublication.Result{}, err
	}
	store, err := agentpublication.NewStore(paths.StateRoot, "gemini/ant-router")
	if err != nil {
		return agentpublication.Result{}, err
	}
	manager, err := agentpublication.NewManager(store, agentpublication.SettingsFile{Path: settingsPath}, "gemini")
	if err != nil {
		return agentpublication.Result{}, err
	}
	return manager.Publish(ctx, input)
}

func executeAgentOffline(ctx context.Context, options cliOptions, command cliCommand, stdout, stderr io.Writer) int {
	if command.Action == "status" {
		return executeAgentStatus(ctx, options, command, stdout, stderr)
	}
	home, err := geminiHome(options)
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	settingsPath := filepath.Join(home, geminiSettingsRelative)
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	store, err := agentpublication.NewStore(paths.StateRoot, "gemini/ant-router")
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	manager, err := agentpublication.NewManager(store, agentpublication.SettingsFile{Path: settingsPath}, "gemini")
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	var result agentpublication.Result
	switch command.Action {
	case "disable":
		result, err = manager.Disable(ctx)
	case "restore":
		result, err = manager.Restore(ctx)
	}
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	if err := writeAgentResult(stdout, options, result, "Gemini CLI settings restored."); err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	return ExitSuccess
}

func executeAgentStatus(ctx context.Context, options cliOptions, command cliCommand, stdout, stderr io.Writer) int {
	home, err := geminiHome(options)
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	settingsPath := filepath.Join(home, geminiSettingsRelative)
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	store, err := agentpublication.NewStore(paths.StateRoot, "gemini/ant-router")
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	manager, err := agentpublication.NewManager(store, agentpublication.SettingsFile{Path: settingsPath}, "gemini")
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	status, err := manager.Status()
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	if options.JSON {
		if err := writeJSONOutput(stdout, map[string]any{"schema_version": cliSchemaVersion, "agent": command.Agent, "published": status.Published, "drifted": status.Drifted}); err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		return ExitSuccess
	}
	state := "not published"
	if status.Published {
		state = "published"
		if status.Drifted {
			state = "published but drifted; republish to reconcile"
		}
	}
	if _, err := fmt.Fprintf(stdout, "Agent: %s\nSettings: %s\nState: %s\n", command.Agent, settingsPath, state); err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	return ExitSuccess
}

func writeAgentResult(stdout io.Writer, options cliOptions, result agentpublication.Result, hint string) error {
	if options.JSON {
		return writeJSONOutput(stdout, map[string]any{"schema_version": cliSchemaVersion, "action": result.Action, "changed": result.Changed, "hint": hint})
	}
	_, err := fmt.Fprintf(stdout, "%s completed.\n%s\n", result.Action, hint)
	return err
}
