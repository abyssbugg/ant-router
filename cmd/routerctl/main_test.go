package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestRunVersionHelpAndUsage(t *testing.T) {
	factory := func(cliOptions) (controlClient, error) { t.Fatal("factory called"); return nil, nil }
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"version"}, bytes.NewReader(nil), &stdout, &stderr, factory); code != 0 || !bytes.Contains(stdout.Bytes(), []byte("ant-router dev")) {
		t.Fatalf("version = code %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"--help"}, bytes.NewReader(nil), &stdout, &stderr, factory); code != 0 {
		t.Errorf("help code = %d", code)
	}
	stderr.Reset()
	if code := run(context.Background(), []string{"bogus"}, bytes.NewReader(nil), &stdout, &stderr, factory); code != ExitUsage || !bytes.Contains(stderr.Bytes(), []byte("invalid command or arguments")) {
		t.Errorf("unknown = code %d stderr %q", code, stderr.String())
	}
}

func TestRunClientFactoryFailure(t *testing.T) {
	sentinel := errors.New("connection failed")
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"status"}, bytes.NewReader(nil), &bytes.Buffer{}, &stderr, func(cliOptions) (controlClient, error) { return nil, sentinel })
	if code != ExitConnection || !bytes.Contains(stderr.Bytes(), []byte("unable to connect to daemon")) {
		t.Errorf("factory failure = code %d stderr %q", code, stderr.String())
	}
}
