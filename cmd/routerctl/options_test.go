package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseCLIOptionsAndCommands(t *testing.T) {
	tests := []struct {
		args   []string
		name   string
		action string
		input  string
		json   bool
	}{
		{args: []string{"status"}, name: "status"},
		{args: []string{"--json", "--instance", "dev", "status"}, name: "status", json: true},
		{args: []string{"config", "current"}, name: "config", action: "current"},
		{args: []string{"config", "validate", "-"}, name: "config", action: "validate", input: "-"},
		{args: []string{"config", "apply", "config.toml"}, name: "config", action: "apply", input: "config.toml"},
		{args: []string{"config", "rollback"}, name: "config", action: "rollback"},
		{args: []string{"version"}, name: "version"},
	}
	for _, test := range tests {
		var stderr bytes.Buffer
		options, command, err := parseCLI(test.args, &stderr)
		if err != nil {
			t.Fatalf("parseCLI(%v) failed: %v", test.args, err)
		}
		if command.Name != test.name || command.Action != test.action || command.Input != test.input || options.JSON != test.json {
			t.Errorf("parseCLI(%v) = %#v %#v", test.args, options, command)
		}
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
	invalid := [][]string{nil, []string{"config"}, []string{"config", "validate"}, []string{"config", "rollback", "extra"}, []string{"status", "extra"}, []string{"--bad"}}
	for _, args := range invalid {
		_, _, err := parseCLI(args, &bytes.Buffer{})
		if !errors.Is(err, errUsage) {
			t.Errorf("parseCLI(%v) error = %v, want errUsage", args, err)
		}
	}
}
