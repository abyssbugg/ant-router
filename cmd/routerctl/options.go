package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"io"
)

var errUsage = errors.New("usage error")

const (
	maxCLIArguments      = 128
	maxCLIArgumentLength = 4096
)

type cliOptions struct {
	Instance, StateRoot, RuntimeRoot string
	CodexHome, CodexBin              string
	AllowUntestedCodex               bool
	JSON                             bool
}

type cliCommand struct {
	Name              string
	Action            string
	Input             string
	ShowToken         bool
	Proof, Generation string
	Args              []string
}

func parseCLI(args []string, stderr io.Writer) (cliOptions, cliCommand, error) {
	if len(args) > maxCLIArguments {
		return cliOptions{}, cliCommand{}, errUsage
	}
	for _, argument := range args {
		if len(argument) > maxCLIArgumentLength {
			return cliOptions{}, cliCommand{}, errUsage
		}
	}
	flags := flag.NewFlagSet("routerctl", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options cliOptions
	flags.StringVar(&options.Instance, "instance", "", "named daemon instance")
	flags.StringVar(&options.StateRoot, "state-dir", "", "persistent state root")
	flags.StringVar(&options.RuntimeRoot, "runtime-dir", "", "local runtime root")
	flags.StringVar(&options.CodexHome, "codex-home", "", "isolated Codex home")
	flags.StringVar(&options.CodexBin, "codex-bin", "", "Codex executable")
	flags.BoolVar(&options.AllowUntestedCodex, "allow-untested-codex", false, "allow an untested newer Codex version")
	flags.BoolVar(&options.JSON, "json", false, "emit JSON output")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options, cliCommand{}, flag.ErrHelp
		}
		return options, cliCommand{}, errors.Join(errUsage, err)
	}
	remaining := flags.Args()
	if len(remaining) == 0 {
		return options, cliCommand{}, errUsage
	}
	command := cliCommand{Name: remaining[0]}
	switch command.Name {
	case "version", "status":
		if len(remaining) != 1 {
			return options, cliCommand{}, errUsage
		}
	case "data-plane":
		if len(remaining) < 2 {
			return options, cliCommand{}, errUsage
		}
		command.Action = remaining[1]
		switch command.Action {
		case "env":
			if len(remaining) > 3 {
				return options, cliCommand{}, errUsage
			}
			if len(remaining) == 3 {
				if remaining[2] != "--show-token" {
					return options, cliCommand{}, errUsage
				}
				command.ShowToken = true
			}
		case "rotate":
			if len(remaining) != 2 {
				return options, cliCommand{}, errUsage
			}
		default:
			return options, cliCommand{}, errUsage
		}
	case "codex":
		if err := parseCodexCommand(remaining, &command); err != nil {
			return options, cliCommand{}, err
		}
	case "config":
		if len(remaining) < 2 {
			return options, cliCommand{}, errUsage
		}
		command.Action = remaining[1]
		switch command.Action {
		case "current", "rollback":
			if len(remaining) != 2 {
				return options, cliCommand{}, errUsage
			}
		case "validate", "apply":
			if len(remaining) != 3 {
				return options, cliCommand{}, errUsage
			}
			command.Input = remaining[2]
		default:
			return options, cliCommand{}, errUsage
		}
	default:
		return options, cliCommand{}, errUsage
	}
	return options, command, nil
}

func parseCodexCommand(arguments []string, command *cliCommand) error {
	if len(arguments) < 2 {
		return errUsage
	}
	action := arguments[1]
	switch action {
	case "token", "status", "doctor":
		if len(arguments) != 2 {
			return errUsage
		}
		command.Action = action
	case "plan":
		if len(arguments) < 3 {
			return errUsage
		}
		switch arguments[2] {
		case "enable", "disable":
			if len(arguments) != 3 {
				return errUsage
			}
			command.Action = "plan-" + arguments[2]
		case "restore":
			if len(arguments) != 5 || arguments[3] != "--generation" || !validCLIProof(arguments[4]) {
				return errUsage
			}
			command.Action, command.Generation = "plan-restore", arguments[4]
		default:
			return errUsage
		}
	case "enable", "disable":
		if len(arguments) != 4 || arguments[2] != "--plan" || !validCLIProof(arguments[3]) {
			return errUsage
		}
		command.Action, command.Proof = action, arguments[3]
	case "restore":
		if len(arguments) != 6 || arguments[2] != "--generation" || !validCLIProof(arguments[3]) || arguments[4] != "--plan" || !validCLIProof(arguments[5]) {
			return errUsage
		}
		command.Action, command.Generation, command.Proof = action, arguments[3], arguments[5]
	case "run":
		values := arguments[2:]
		if len(values) > 0 && values[0] == "--" {
			values = values[1:]
		}
		if len(values) == 0 {
			return errUsage
		}
		command.Action, command.Args = action, append([]string(nil), values...)
	default:
		return errUsage
	}
	return nil
}

func validCLIProof(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
