package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
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

func TestRunCodexTokenDoesNotConstructControlClient(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := inferencetoken.NewStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.LoadOrCreate(controltransport.DefaultInstance)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	factory := func(cliOptions) (controlClient, error) { t.Fatal("control client factory called"); return nil, nil }
	code := run(context.Background(), []string{"--state-dir", stateRoot, "codex", "token"}, bytes.NewReader(nil), &stdout, &stderr, factory)
	if code != 0 || stdout.String() != token.String()+"\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunCodexPlanDisableDoesNotConstructControlClient(t *testing.T) {
	stateRoot, codexHome := t.TempDir(), t.TempDir()
	if err := os.Chmod(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	factory := func(cliOptions) (controlClient, error) { t.Fatal("control client factory called"); return nil, nil }
	code := run(context.Background(), []string{"--state-dir", stateRoot, "--codex-home", codexHome, "codex", "plan", "disable"}, bytes.NewReader(nil), &stdout, &stderr, factory)
	if code != 0 || !bytes.Contains(stdout.Bytes(), []byte("Proof:")) || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
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
