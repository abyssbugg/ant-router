package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
	"github.com/abyssbugg/ant-router/internal/codexintegration"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/runtimepaths"
)

type controlClient interface {
	Status(context.Context) (control.StatusResponse, error)
	CodexCatalog(context.Context) (control.CodexCatalogResponse, error)
	CurrentConfig(context.Context) (control.ConfigCurrentResponse, error)
	ValidateConfig(context.Context, []byte) (control.ConfigValidateResponse, error)
	ApplyConfig(context.Context, []byte, control.ConfigValidateResponse) (control.ConfigMutationResponse, error)
	RollbackConfig(context.Context) (control.ConfigMutationResponse, error)
	InferenceToken(context.Context) (inferencetoken.Token, error)
	CloseIdleConnections()
}

func execute(ctx context.Context, options cliOptions, command cliCommand, stdin io.Reader, stdout, stderr io.Writer, client controlClient) int {
	var err error
	switch command.Name {
	case "status":
		var result control.StatusResponse
		result, err = client.Status(ctx)
		if err == nil {
			err = writeStatus(stdout, options, result)
		}
	case "data-plane":
		var status control.StatusResponse
		status, err = client.Status(ctx)
		if err == nil {
			if status.DataPlane == nil || !status.DataPlane.Enabled {
				err = errDataPlaneUnavailable
			} else {
				var token *inferencetoken.Token
				if command.ShowToken {
					var value inferencetoken.Token
					value, err = client.InferenceToken(ctx)
					token = &value
				}
				if err == nil {
					err = writeDataPlaneEnv(stdout, options, status.DataPlane.BaseURL, token)
				}
			}
		}
	case "codex":
		return executeCodex(ctx, options, command, stdin, stdout, stderr, client)
	case "config":
		switch command.Action {
		case "current":
			var result control.ConfigCurrentResponse
			result, err = client.CurrentConfig(ctx)
			if err == nil {
				err = writeCurrent(stdout, options, result)
			}
		case "validate":
			var data []byte
			data, err = readConfiguration(command.Input, stdin)
			if err == nil {
				var result control.ConfigValidateResponse
				result, err = client.ValidateConfig(ctx, data)
				if err == nil {
					err = writeValidation(stdout, options, result)
				}
			}
		case "apply":
			var data []byte
			data, err = readConfiguration(command.Input, stdin)
			if err == nil {
				var proof control.ConfigValidateResponse
				proof, err = client.ValidateConfig(ctx, data)
				if err == nil {
					var result control.ConfigMutationResponse
					result, err = client.ApplyConfig(ctx, data, proof)
					if err == nil {
						err = writeMutation(stdout, options, "apply", result)
					}
				}
			}
		case "rollback":
			var result control.ConfigMutationResponse
			result, err = client.RollbackConfig(ctx)
			if err == nil {
				err = writeMutation(stdout, options, "rollback", result)
			}
		}
	}
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	return ExitSuccess
}

// executeDataPlaneRotate regenerates the inference token offline. The daemon
// serves data-plane authentication from the current on-disk token, so the new
// credential takes effect immediately and the previous one stops working.
func executeDataPlaneRotate(options cliOptions, stdout, stderr io.Writer) int {
	paths, err := runtimepaths.Resolve(runtimepaths.Options{InstanceOverride: options.Instance, StateOverride: options.StateRoot, RuntimeOverride: options.RuntimeRoot})
	if err == nil {
		var store *inferencetoken.Store
		store, err = inferencetoken.NewStore(paths.StateRoot)
		if err == nil {
			_, err = store.Rotate(paths.Instance)
		}
	}
	if err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	if options.JSON {
		if err := writeJSONOutput(stdout, map[string]any{"schema_version": cliSchemaVersion, "rotated": true}); err != nil {
			_ = writeCLIError(stderr, options, err)
			return exitCode(err)
		}
		return ExitSuccess
	}
	if _, err := fmt.Fprintln(stdout, "Inference token rotated."); err != nil {
		_ = writeCLIError(stderr, options, err)
		return exitCode(err)
	}
	return ExitSuccess
}

func writeVersion(writer io.Writer, options cliOptions) error {
	info := buildinfo.Current()
	if options.JSON {
		return writeJSONOutput(writer, map[string]any{"schema_version": cliSchemaVersion, "name": "ant-router", "version": info.Version, "commit": info.Commit, "built_at": info.Date})
	}
	_, err := fmt.Fprintf(writer, "ant-router %s (commit %s, built %s)\n", info.Version, info.Commit, info.Date)
	return err
}

type cliErrorDetail struct {
	ExitCode int    `json:"exit_code"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type cliErrorEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	Error         cliErrorDetail `json:"error"`
}

func writeCLIError(writer io.Writer, options cliOptions, err error) error {
	detail := describeCLIError(err)
	if options.JSON {
		return writeJSONOutput(writer, cliErrorEnvelope{SchemaVersion: cliSchemaVersion, Error: detail})
	}
	_, writeErr := fmt.Fprintf(writer, "ant-router: %s\n", detail.Message)
	return writeErr
}

func describeCLIError(err error) cliErrorDetail {
	exit := exitCode(err)
	code := "internal"
	message := "operation failed"
	switch exit {
	case ExitUsage:
		code, message = "usage", "invalid command or arguments"
		var ctErr *controltransport.Error
		if errors.As(err, &ctErr) && ctErr != nil && ctErr.Code == controltransport.CodeInvalidInstance {
			message = "invalid instance name"
		}
	case ExitConnection:
		code, message = "connection", "unable to connect to daemon"
	case ExitAuthentication:
		code, message = "authentication", "authentication failed"
	case ExitValidation:
		code, message = "validation", "configuration is invalid"
		if errors.Is(err, codexintegration.ErrInvalid) {
			message = "Codex integration request is invalid"
		}
	case ExitConflict:
		code, message = "conflict", "configuration changed; validate again"
		if errors.Is(err, codexintegration.ErrConflict) {
			message = "Codex integration changed; plan again"
		}
	case ExitUnavailable:
		code, message = "unavailable", "configuration unavailable"
		if errors.Is(err, codexintegration.ErrUnavailable) {
			message = "Codex integration is unavailable"
		}
		if errors.Is(err, errDataPlaneUnavailable) {
			message = "data plane is unavailable"
		}
	}
	return cliErrorDetail{ExitCode: exit, Code: code, Message: message}
}
