package main

import (
	"bytes"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := run([]string{"version"}, stdout, stderr)

	if code != 0 {
		t.Errorf("version command: expected exit code 0, got %d", code)
	}

	expected := "ant-router dev (commit unknown, built unknown)\n"
	if stdout.String() != expected {
		t.Errorf("version command: expected stdout %q, got %q", expected, stdout.String())
	}

	if stderr.String() != "" {
		t.Errorf("version command should not write to stderr, got: %q", stderr.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := run([]string{"bogus"}, stdout, stderr)

	if code == 0 {
		t.Errorf("unknown command: expected nonzero exit code, got %d", code)
	}

	expected := "ant-router: unknown command \"bogus\"\n"
	if stderr.String() != expected {
		t.Errorf("unknown command: expected stderr %q, got %q", expected, stderr.String())
	}
}
