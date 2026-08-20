package main

import (
	"errors"
	"flag"
	"io"
)

var errUsage = errors.New("usage error")

type cliOptions struct {
	Instance    string
	StateRoot   string
	RuntimeRoot string
	JSON        bool
}

type cliCommand struct {
	Name   string
	Action string
	Input  string
}

func parseCLI(args []string, stderr io.Writer) (cliOptions, cliCommand, error) {
	flags := flag.NewFlagSet("routerctl", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options cliOptions
	flags.StringVar(&options.Instance, "instance", "", "named daemon instance")
	flags.StringVar(&options.StateRoot, "state-dir", "", "persistent state root")
	flags.StringVar(&options.RuntimeRoot, "runtime-dir", "", "local runtime root")
	flags.BoolVar(&options.JSON, "json", false, "emit JSON output")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cliOptions{}, cliCommand{}, flag.ErrHelp
		}
		return cliOptions{}, cliCommand{}, errors.Join(errUsage, err)
	}
	remaining := flags.Args()
	if len(remaining) == 0 {
		return cliOptions{}, cliCommand{}, errUsage
	}
	command := cliCommand{Name: remaining[0]}
	switch command.Name {
	case "version", "status":
		if len(remaining) != 1 {
			return cliOptions{}, cliCommand{}, errUsage
		}
	case "config":
		if len(remaining) < 2 {
			return cliOptions{}, cliCommand{}, errUsage
		}
		command.Action = remaining[1]
		switch command.Action {
		case "current", "rollback":
			if len(remaining) != 2 {
				return cliOptions{}, cliCommand{}, errUsage
			}
		case "validate", "apply":
			if len(remaining) != 3 {
				return cliOptions{}, cliCommand{}, errUsage
			}
			command.Input = remaining[2]
		default:
			return cliOptions{}, cliCommand{}, errUsage
		}
	default:
		return cliOptions{}, cliCommand{}, errUsage
	}
	return options, command, nil
}
