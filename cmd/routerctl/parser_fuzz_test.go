package main

import (
	"io"
	"strings"
	"testing"
)

func FuzzParseCLI(f *testing.F) {
	f.Add("status")
	f.Add("--json config validate -")
	f.Add("config")
	f.Add("--bad")
	f.Fuzz(func(t *testing.T, commandLine string) {
		options, command, err := parseCLI(strings.Fields(commandLine), io.Discard)
		if err != nil {
			return
		}
		_ = options
		switch command.Name {
		case "version", "status":
			if command.Action != "" || command.Input != "" {
				t.Fatalf("invalid simple command: %#v", command)
			}
		case "config":
			switch command.Action {
			case "current", "rollback":
				if command.Input != "" {
					t.Fatalf("unexpected config input: %#v", command)
				}
			case "validate", "apply":
				if command.Input == "" {
					t.Fatalf("missing config input: %#v", command)
				}
			default:
				t.Fatalf("invalid config action: %#v", command)
			}
		default:
			t.Fatalf("invalid command: %#v", command)
		}
	})
}
