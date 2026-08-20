package daemon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
)

type staticTokenSource struct{ token controltoken.Token }

func (s staticTokenSource) Token(context.Context) (controltoken.Token, error) { return s.token, nil }

func daemonOptions(t *testing.T, stateRoot, runtimeRoot string) Options {
	t.Helper()
	return Options{
		Instance:    "bootstrap",
		StateRoot:   stateRoot,
		RuntimeRoot: runtimeRoot,
		Build:       buildinfo.Info{Version: "test", Commit: "abc", Date: "2026-08-19"},
	}
}

func shortRuntimeRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "ar-daemon-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func startDaemon(t *testing.T, options Options) (*Daemon, context.CancelFunc, <-chan error) {
	t.Helper()
	d, err := New(options)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case <-d.Ready():
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("daemon did not become ready")
	}
	return d, cancel, done
}

func daemonClient(t *testing.T, d *Daemon) *control.Client {
	t.Helper()
	client, err := control.NewClient(controltransport.LocalDialer{Endpoint: d.endpoint}, staticTokenSource{token: d.token}, control.DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func transportCode(err error) controltransport.Code {
	code, _ := controltransport.CodeOf(err)
	return code
}

func TestDaemonBootstrapApplyAndRestart(t *testing.T) {
	stateRoot := t.TempDir()
	runtimeRoot := shortRuntimeRoot(t)
	options := daemonOptions(t, stateRoot, runtimeRoot)

	first, cancelFirst, firstDone := startDaemon(t, options)
	client := daemonClient(t, first)
	current, err := client.CurrentConfig(context.Background())
	if err != nil || current.State != "awaiting_configuration" {
		t.Fatalf("initial CurrentConfig() = %#v, %v", current, err)
	}
	tomlBytes := []byte("schema_version = 1\n")
	proof, err := client.ValidateConfig(context.Background(), tomlBytes)
	if err != nil {
		t.Fatalf("ValidateConfig() failed: %v", err)
	}
	applied, err := client.ApplyConfig(context.Background(), tomlBytes, proof)
	if err != nil || applied.State != "running" || applied.Generation != 1 {
		t.Fatalf("ApplyConfig() = %#v, %v", applied, err)
	}
	cancelFirst()
	if err := <-firstDone; err != nil {
		t.Fatalf("first daemon Run() failed: %v", err)
	}

	second, cancelSecond, secondDone := startDaemon(t, options)
	restartedClient := daemonClient(t, second)
	restored, err := restartedClient.CurrentConfig(context.Background())
	if err != nil || restored.State != "running" || restored.Revision != applied.Revision || restored.Generation != 1 {
		t.Fatalf("restored CurrentConfig() = %#v, %v", restored, err)
	}
	cancelSecond()
	if err := <-secondDone; err != nil {
		t.Fatalf("second daemon Run() failed: %v", err)
	}
}

func TestDaemonEndpointCollision(t *testing.T) {
	options := daemonOptions(t, t.TempDir(), shortRuntimeRoot(t))
	first, cancel, done := startDaemon(t, options)
	defer func() { cancel(); <-done }()
	if _, err := New(options); transportCode(err) != controltransport.CodeAlreadyRunning {
		t.Fatalf("collision error = %v", err)
	}
	_ = first
}
