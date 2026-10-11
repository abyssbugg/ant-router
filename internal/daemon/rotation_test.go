package daemon

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

func newTokenClient(t *testing.T, d *Daemon, token controltoken.Token) *control.Client {
	t.Helper()
	client, err := control.NewClient(controltransport.LocalDialer{Endpoint: d.endpoint}, staticTokenSource{token: token}, control.DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func TestControlTokenRotationTakesEffectWithoutRestart(t *testing.T) {
	stateRoot := t.TempDir()
	runtimeRoot := shortRuntimeRoot(t)
	d, cancel, done := startDaemon(t, daemonOptions(t, stateRoot, runtimeRoot))
	if _, err := daemonClient(t, d).Status(context.Background()); err != nil {
		t.Fatalf("initial Status() failed: %v", err)
	}
	store, err := controltoken.NewStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := store.Rotate(d.paths.Instance)
	if err != nil {
		t.Fatalf("Rotate() failed: %v", err)
	}
	if _, err := newTokenClient(t, d, d.token).Status(context.Background()); err == nil {
		t.Fatal("stale control token still accepted after rotation")
	}
	if _, err := newTokenClient(t, d, rotated).Status(context.Background()); err != nil {
		t.Fatalf("rotated control token rejected: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("daemon Run() failed: %v", err)
	}
}

func dataPlaneModelsStatus(t *testing.T, baseURL, token string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET /models failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

func TestInferenceTokenRotationTakesEffectWithoutRestart(t *testing.T) {
	stateRoot := t.TempDir()
	runtimeRoot := shortRuntimeRoot(t)
	listenConfig := net.ListenConfig{}
	dataListener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataListener.Close() })
	address := dataListener.Addr().String()
	configTOML := []byte("schema_version = 2\n[data_plane]\nenabled = true\nlisten = \"" + address + "\"\nmax_concurrent_requests = 4\nshutdown_timeout = \"2s\"\n[mock_provider]\nenabled = true\n")

	bootstrapOptions := daemonOptions(t, stateRoot, runtimeRoot)
	bootstrap, cancelBootstrap, bootstrapDone := startDaemon(t, bootstrapOptions)
	client := daemonClient(t, bootstrap)
	proof, err := client.ValidateConfig(context.Background(), configTOML)
	if err != nil {
		t.Fatalf("ValidateConfig() failed: %v", err)
	}
	if _, err := client.ApplyConfig(context.Background(), configTOML, proof); err != nil {
		t.Fatalf("ApplyConfig() failed: %v", err)
	}
	cancelBootstrap()
	if err := <-bootstrapDone; err != nil {
		t.Fatalf("bootstrap daemon Run() failed: %v", err)
	}

	options := daemonOptions(t, stateRoot, runtimeRoot)
	options.DataPlaneListen = func(requested string) (net.Listener, error) {
		if requested != address {
			t.Fatalf("data-plane listen request = %q", requested)
		}
		return dataListener, nil
	}
	d, cancel, done := startDaemon(t, options)

	store, err := inferencetoken.NewStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Load(d.paths.Instance)
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "http://" + address + "/v1"
	if status := dataPlaneModelsStatus(t, baseURL, current.String()); status != http.StatusOK {
		t.Fatalf("GET /models with current token = %d", status)
	}
	rotated, err := store.Rotate(d.paths.Instance)
	if err != nil {
		t.Fatalf("Rotate() failed: %v", err)
	}
	if status := dataPlaneModelsStatus(t, baseURL, current.String()); status != http.StatusUnauthorized {
		t.Fatalf("GET /models with stale token = %d, want 401", status)
	}
	if status := dataPlaneModelsStatus(t, baseURL, rotated.String()); status != http.StatusOK {
		t.Fatalf("GET /models with rotated token = %d, want 200", status)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("running daemon Run() failed: %v", err)
	}
}
