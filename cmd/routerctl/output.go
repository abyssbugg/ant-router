package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/abyssbugg/ant-router/internal/control"
)

const cliSchemaVersion = 1

func writeJSONOutput(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	return encoder.Encode(value)
}

func writeStatus(writer io.Writer, options cliOptions, status control.StatusResponse) error {
	if options.JSON {
		return writeJSONOutput(writer, map[string]any{"schema_version": cliSchemaVersion, "instance": status.Instance, "daemon_state": status.DaemonState, "started_at": status.StartedAt, "uptime_seconds": status.UptimeSeconds, "config": status.Config})
	}
	_, err := fmt.Fprintf(writer, "Instance: %s\nState: %s\nUptime: %s\n", status.Instance, status.DaemonState, (time.Duration(status.UptimeSeconds) * time.Second).String())
	return err
}

func writeCurrent(writer io.Writer, options cliOptions, current control.ConfigCurrentResponse) error {
	if options.JSON {
		return writeJSONOutput(writer, map[string]any{"schema_version": cliSchemaVersion, "state": current.State, "revision": current.Revision, "generation": current.Generation, "status": current.Status, "activated_at": current.ActivatedAt})
	}
	if current.Revision == "" {
		_, err := fmt.Fprintf(writer, "Configuration state: %s\n", current.State)
		return err
	}
	_, err := fmt.Fprintf(writer, "Configuration state: %s\nRevision: %s\nGeneration: %d\nStatus: %s\n", current.State, current.Revision, current.Generation, current.Status)
	return err
}

func writeValidation(writer io.Writer, options cliOptions, result control.ConfigValidateResponse) error {
	if options.JSON {
		return writeJSONOutput(writer, map[string]any{"schema_version": cliSchemaVersion, "valid": true, "revision": result.Revision, "base_generation": result.BaseGeneration, "canonical_size": result.CanonicalSize})
	}
	_, err := fmt.Fprintf(writer, "Configuration valid\nRevision: %s\nGeneration: %d\nSize: %d bytes\n", result.Revision, result.BaseGeneration, result.CanonicalSize)
	return err
}

func writeMutation(writer io.Writer, options cliOptions, action string, result control.ConfigMutationResponse) error {
	if options.JSON {
		return writeJSONOutput(writer, map[string]any{"schema_version": cliSchemaVersion, "action": action, "state": result.State, "revision": result.Revision, "generation": result.Generation, "status": result.Status, "activated_at": result.ActivatedAt})
	}
	label := "Configuration activated"
	if action == "rollback" {
		label = "Configuration rolled back"
	}
	_, err := fmt.Fprintf(writer, "%s\nRevision: %s\nGeneration: %d\n", label, result.Revision, result.Generation)
	return err
}
