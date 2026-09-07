package main

import (
	"context"
	"fmt"
	"io"

	"github.com/abyssbugg/ant-router/internal/codexintegration"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
)

func executeCodexToken(ctx context.Context, options cliOptions, stdout, stderr io.Writer) int {
	if options.JSON {
		_ = writeCLIError(stderr, options, errUsage)
		return ExitUsage
	}
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	store, err := inferencetoken.NewStore(paths.StateRoot)
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	token, err := store.Load(paths.Instance)
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	if _, err := fmt.Fprintln(stdout, token.String()); err != nil {
		_ = writeCLIError(stderr, options, err)
		return ExitInternal
	}
	return ExitSuccess
}

func executeCodex(ctx context.Context, options cliOptions, command cliCommand, stdin io.Reader, stdout, stderr io.Writer, client controlClient) int {
	if command.Action == "token" {
		if options.JSON {
			_ = writeCLIError(stderr, options, errUsage)
			return ExitUsage
		}
		token, err := client.InferenceToken(ctx)
		if err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		if _, err := fmt.Fprintln(stdout, token.String()); err != nil {
			_ = writeCLIError(stderr, options, err)
			return ExitInternal
		}
		return ExitSuccess
	}
	service, err := newCodexService(options, client)
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	return executeCodexService(ctx, options, command, stdin, stdout, stderr, service)
}

func executeCodexService(ctx context.Context, options cliOptions, command cliCommand, stdin io.Reader, stdout, stderr io.Writer, service codexService) int {
	switch command.Action {
	case "run":
		if options.JSON {
			_ = writeCLIError(stderr, options, errUsage)
			return ExitUsage
		}
		code, err := service.Run(ctx, command.Args, stdin, stdout, stderr)
		if err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		return code
	case "status":
		status, err := service.Status(ctx)
		if err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		if err := writeCodexStatus(stdout, options, command.Action, status); err != nil {
			_ = writeCLIError(stderr, options, err)
			return ExitInternal
		}
		return ExitSuccess
	case "doctor":
		status, doctorErr := service.Doctor(ctx)
		if status.State != "" {
			if err := writeCodexStatus(stdout, options, command.Action, status); err != nil {
				_ = writeCLIError(stderr, options, err)
				return ExitInternal
			}
		}
		if doctorErr != nil {
			_ = writeCLIError(stderr, options, doctorErr)
			return exitCode(doctorErr)
		}
		return ExitSuccess
	}
	action, isPlan := codexAction(command.Action)
	if action == "" {
		_ = writeCLIError(stderr, options, errUsage)
		return ExitUsage
	}
	if isPlan {
		plan, err := service.Plan(ctx, action, command.Generation)
		if err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		if err := writeCodexPlan(stdout, options, plan); err != nil {
			_ = writeCLIError(stderr, options, err)
			return ExitInternal
		}
		return ExitSuccess
	}
	result, err := service.Apply(ctx, action, command.Generation, command.Proof)
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	if err := writeCodexResult(stdout, options, result); err != nil {
		_ = writeCLIError(stderr, options, err)
		return ExitInternal
	}
	return ExitSuccess
}

func offlineCodexAction(action string) bool {
	switch action {
	case "plan-disable", "disable", "plan-restore", "restore", "run":
		return true
	default:
		return false
	}
}

func codexAction(value string) (codexintegration.Action, bool) {
	switch value {
	case "plan-enable":
		return codexintegration.ActionEnable, true
	case "plan-disable":
		return codexintegration.ActionDisable, true
	case "plan-restore":
		return codexintegration.ActionRestore, true
	case "enable":
		return codexintegration.ActionEnable, false
	case "disable":
		return codexintegration.ActionDisable, false
	case "restore":
		return codexintegration.ActionRestore, false
	default:
		return "", false
	}
}

func writeCodexPlan(writer io.Writer, options cliOptions, plan codexintegration.Plan) error {
	if options.JSON {
		return writeJSONOutput(writer, map[string]any{"schema_version": cliSchemaVersion, "action": plan.Action(), "plan": plan.Digest(), "profile_path": plan.ProfilePath(), "catalog_path": plan.CatalogPath(), "codex_version": plan.CodexVersion(), "config_generation": plan.ConfigGeneration(), "ownership_generation": plan.OwnershipGeneration(), "changes": plan.Changes()})
	}
	if _, err := fmt.Fprintf(writer, "Codex %s plan\nProof: %s\nProfile: %s\nCatalog: %s\nCodex: %s\n", plan.Action(), plan.Digest(), plan.ProfilePath(), plan.CatalogPath(), plan.CodexVersion()); err != nil {
		return err
	}
	for _, change := range plan.Changes() {
		if _, err := fmt.Fprintf(writer, "%s %s\n", change.Kind, change.Path); err != nil {
			return err
		}
	}
	return nil
}

func writeCodexResult(writer io.Writer, options cliOptions, result codexintegration.Result) error {
	if options.JSON {
		return writeJSONOutput(writer, map[string]any{"schema_version": cliSchemaVersion, "action": result.Action, "generation": result.GenerationID, "changed": result.Changed, "restart_codex": result.Changed})
	}
	_, err := fmt.Fprintf(writer, "Codex %s complete\nGeneration: %s\nChanged: %t\n", result.Action, result.GenerationID, result.Changed)
	if err == nil && result.Changed {
		_, err = fmt.Fprintln(writer, "Restart Codex to load the profile and model catalog.")
	}
	return err
}

func writeCodexStatus(writer io.Writer, options cliOptions, action string, status codexServiceStatus) error {
	if options.JSON {
		return writeJSONOutput(writer, map[string]any{"schema_version": cliSchemaVersion, "check": action, "state": status.State, "profile_path": status.ProfilePath, "catalog_path": status.CatalogPath, "codex_version": status.CodexVersion, "pending_changes": status.Changes})
	}
	_, err := fmt.Fprintf(writer, "Codex integration: %s\nCodex: %s\nProfile: %s\nCatalog: %s\nPending changes: %d\n", status.State, status.CodexVersion, status.ProfilePath, status.CatalogPath, status.Changes)
	return err
}
