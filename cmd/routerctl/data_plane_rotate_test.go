package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

func TestDataPlaneRotateRewritesTokenFile(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := inferencetoken.NewStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.LoadOrCreate(controltransport.DefaultInstance)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	factory := func(cliOptions) (controlClient, error) {
		t.Fatal("control client factory called for offline rotate")
		return nil, nil
	}
	code := run(context.Background(), []string{"--state-dir", stateRoot, "data-plane", "rotate"}, strings.NewReader(""), &stdout, &stderr, factory)
	if code != ExitSuccess {
		t.Fatalf("run() = %d, stderr = %q", code, stderr.String())
	}
	rotated, err := store.Load(controltransport.DefaultInstance)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == original {
		t.Fatal("token file did not change")
	}
	if original.Verify(rotated.String()) || !rotated.Verify(rotated.String()) {
		t.Fatal("rotated token is not a fresh independent credential")
	}
	stdout.Reset()
	code = run(context.Background(), []string{"--state-dir", stateRoot, "--json", "data-plane", "rotate"}, strings.NewReader(""), &stdout, &stderr, factory)
	if code != ExitSuccess || !bytes.Contains(stdout.Bytes(), []byte(`"rotated":true`)) {
		t.Fatalf("json rotate = %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestParseRejectsDataPlaneRotateWithArguments(t *testing.T) {
	var stderr bytes.Buffer
	if _, _, err := parseCLI([]string{"data-plane", "rotate", "--show-token"}, &stderr); err == nil {
		t.Fatal("rotate with arguments accepted")
	}
}
