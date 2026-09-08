package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseCLIOptionsAndCommands(t *testing.T) {
	tests := []struct {
		args        []string
		name        string
		action      string
		input       string
		proof       string
		generation  string
		commandArgs []string
		json        bool
	}{
		{args: []string{"status"}, name: "status"},
		{args: []string{"--json", "--instance", "dev", "status"}, name: "status", json: true},
		{args: []string{"config", "current"}, name: "config", action: "current"},
		{args: []string{"config", "validate", "-"}, name: "config", action: "validate", input: "-"},
		{args: []string{"config", "apply", "config.toml"}, name: "config", action: "apply", input: "config.toml"},
		{args: []string{"config", "rollback"}, name: "config", action: "rollback"},
		{args: []string{"version"}, name: "version"},
		{args: []string{"codex", "token"}, name: "codex", action: "token"},
		{args: []string{"codex", "status"}, name: "codex", action: "status"},
		{args: []string{"codex", "doctor"}, name: "codex", action: "doctor"},
		{args: []string{"codex", "plan", "enable"}, name: "codex", action: "plan-enable"},
		{args: []string{"codex", "enable", "--plan", strings.Repeat("a", 64)}, name: "codex", action: "enable", proof: strings.Repeat("a", 64)},
		{args: []string{"codex", "plan", "restore", "--generation", strings.Repeat("b", 64)}, name: "codex", action: "plan-restore", generation: strings.Repeat("b", 64)},
		{args: []string{"codex", "run", "--", "exec", "hello"}, name: "codex", action: "run", commandArgs: []string{"exec", "hello"}},
	}
	for _, test := range tests {
		var stderr bytes.Buffer
		options, command, err := parseCLI(test.args, &stderr)
		if err != nil {
			t.Fatalf("parseCLI(%v) failed: %v", test.args, err)
		}
		if command.Name != test.name || command.Action != test.action || command.Input != test.input || command.Proof != test.proof || command.Generation != test.generation || !slices.Equal(command.Args, test.commandArgs) || options.JSON != test.json {
			t.Errorf("parseCLI(%v) = %#v %#v", test.args, options, command)
		}
	}
}

func TestParseCodexGlobalOptions(t *testing.T) {
	options, command, err := parseCLI([]string{"--codex-home", "/private/codex", "--codex-bin", "/usr/local/bin/codex", "--allow-untested-codex", "codex", "status"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if options.CodexHome != "/private/codex" || options.CodexBin != "/usr/local/bin/codex" || !options.AllowUntestedCodex || command.Action != "status" {
		t.Fatalf("options=%#v command=%#v", options, command)
	}
}

func TestParseCLIUsageErrorPreservesJSONOption(t *testing.T) {
	options, _, err := parseCLI([]string{"--json", "bogus"}, &bytes.Buffer{})
	if !errors.Is(err, errUsage) {
		t.Fatalf("error = %v, want errUsage", err)
	}
	if !options.JSON {
		t.Error("JSON option was lost on usage error")
	}
}

func TestParseCLIUsageErrors(t *testing.T) {
	invalid := [][]string{nil, {"config"}, {"config", "validate"}, {"config", "rollback", "extra"}, {"status", "extra"}, {"--bad"}, {"codex"}, {"codex", "enable"}, {"codex", "enable", "--plan", "bad"}, {"codex", "plan", "restore"}, {"codex", "run"}, {"codex", "token", "extra"}}
	for _, args := range invalid {
		_, _, err := parseCLI(args, &bytes.Buffer{})
		if !errors.Is(err, errUsage) {
			t.Errorf("parseCLI(%v) error = %v, want errUsage", args, err)
		}
	}
}
