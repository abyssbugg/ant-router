package main

import (
	"context"
	"fmt"
	"io"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
	"github.com/abyssbugg/ant-router/internal/control"
)

type controlClient interface {
	Status(context.Context) (control.StatusResponse, error)
	CurrentConfig(context.Context) (control.ConfigCurrentResponse, error)
	ValidateConfig(context.Context, []byte) (control.ConfigValidateResponse, error)
	ApplyConfig(context.Context, []byte, control.ConfigValidateResponse) (control.ConfigMutationResponse, error)
	RollbackConfig(context.Context) (control.ConfigMutationResponse, error)
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
	case ExitConnection:
		code, message = "connection", "unable to connect to daemon"
	case ExitAuthentication:
		code, message = "authentication", "authentication failed"
	case ExitValidation:
		code, message = "validation", "configuration is invalid"
	case ExitConflict:
		code, message = "conflict", "configuration changed; validate again"
	case ExitUnavailable:
		code, message = "unavailable", "configuration unavailable"
	}
	return cliErrorDetail{ExitCode: exit, Code: code, Message: message}
}
