package main

import (
	"errors"
	"flag"
	"io"
)

var errUsage = errors.New("usage error")

const (
	maxCLIArguments      = 16
	maxCLIArgumentLength = 4096
)

type cliOptions struct {
	Instance    string
	StateRoot   string
	RuntimeRoot string
	JSON        bool
}

type cliCommand struct {
	Name      string
	Action    string
	Input     string
	ShowToken bool
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
		if len(remaining) < 2 || remaining[1] != "env" || len(remaining) > 3 {
			return options, cliCommand{}, errUsage
		}
		command.Action = "env"
		if len(remaining) == 3 {
			if remaining[2] != "--show-token" {
				return options, cliCommand{}, errUsage
			}
			command.ShowToken = true
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
