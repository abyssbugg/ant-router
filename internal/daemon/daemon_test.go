package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
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
	finished := make(chan struct{})
	go func() {
		done <- d.Run(ctx)
		close(finished)
	}()
	// Stop the daemon even when a test fails early, so its listeners, lock,
	// and goroutines never leak into later tests.
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("daemon did not stop during test cleanup")
		}
	})
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

func TestDaemonStartsConfiguredInferenceDataPlane(t *testing.T) {
	stateRoot := t.TempDir()
	runtimeRoot := shortRuntimeRoot(t)
	options := daemonOptions(t, stateRoot, runtimeRoot)
	listenConfig := net.ListenConfig{}
	dataListener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataListener.Close() })
	address := dataListener.Addr().String()
	listenerUsed := false
	options.DataPlaneListen = func(requested string) (net.Listener, error) {
		if requested != address || listenerUsed {
			t.Fatalf("data-plane listen request = %q used %t", requested, listenerUsed)
		}
		listenerUsed = true
		return dataListener, nil
	}

	first, cancelFirst, firstDone := startDaemon(t, options)
	client := daemonClient(t, first)
	tomlBytes := []byte("schema_version = 2\n[data_plane]\nenabled = true\nlisten = \"" + address + "\"\nmax_concurrent_requests = 4\nshutdown_timeout = \"2s\"\n[mock_provider]\nenabled = true\n")
	proof, err := client.ValidateConfig(context.Background(), tomlBytes)
	if err != nil {
		t.Fatalf("ValidateConfig() failed: %v", err)
	}
	if _, err := client.ApplyConfig(context.Background(), tomlBytes, proof); err != nil {
		t.Fatalf("ApplyConfig() failed: %v", err)
	}
	cancelFirst()
	if err := <-firstDone; err != nil {
		t.Fatalf("bootstrap daemon failed: %v", err)
	}

	second, cancelSecond, secondDone := startDaemon(t, options)
	status, err := daemonClient(t, second).Status(context.Background())
	if err != nil || status.DataPlane == nil || status.DataPlane.BaseURL != "http://"+address+"/v1" {
		t.Fatalf("Status() = %#v, %v", status, err)
	}
	store, err := inferencetoken.NewStore(stateRoot)
	if err != nil {
		t.Fatalf("NewStore() failed: %v", err)
	}
	token, err := store.Load(second.paths.Instance)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, status.DataPlane.BaseURL+"/models", nil)
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token.String())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /models failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /models status = %d", response.StatusCode)
	}

	cancelSecond()
	if err := <-secondDone; err != nil {
		t.Fatalf("configured daemon failed: %v", err)
	}
}

func TestDaemonReportsMissingOpenAICredentialAndOmitsItsModels(t *testing.T) {
	stateRoot := t.TempDir()
	runtimeRoot := shortRuntimeRoot(t)
	options := daemonOptions(t, stateRoot, runtimeRoot)
	options.CredentialSource = credential.NewEnvSource(func(string) (string, bool) { return "", false })
	listenConfig := net.ListenConfig{}
	dataListener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataListener.Close() })
	address := dataListener.Addr().String()
	listenerUsed := false
	options.DataPlaneListen = func(requested string) (net.Listener, error) {
		if requested != address || listenerUsed {
			t.Fatalf("data-plane listen request = %q used %t", requested, listenerUsed)
		}
		listenerUsed = true
		return dataListener, nil
	}

	first, cancelFirst, firstDone := startDaemon(t, options)
	client := daemonClient(t, first)
	tomlBytes := []byte("schema_version = 3\n[data_plane]\nenabled = true\nlisten = \"" + address + "\"\nmax_concurrent_requests = 4\nshutdown_timeout = \"2s\"\n[[providers]]\nid = \"mock\"\nkind = \"mock\"\nenabled = true\n[[providers]]\nid = \"openai-primary\"\nkind = \"openai\"\nenabled = true\n[providers.credential]\nsource = \"env\"\nname = \"OPENAI_API_KEY\"\n[[providers.models]]\nalias = \"openai/gpt-test\"\nupstream = \"gpt-test\"\ncapabilities = [\"text\"]\n")
	proof, err := client.ValidateConfig(context.Background(), tomlBytes)
	if err != nil {
		t.Fatalf("ValidateConfig() failed: %v", err)
	}
	if _, err := client.ApplyConfig(context.Background(), tomlBytes, proof); err != nil {
		t.Fatalf("ApplyConfig() failed: %v", err)
	}
	cancelFirst()
	if err := <-firstDone; err != nil {
		t.Fatalf("bootstrap daemon failed: %v", err)
	}

	second, cancelSecond, secondDone := startDaemon(t, options)
	defer func() {
		cancelSecond()
		if err := <-secondDone; err != nil {
			t.Errorf("configured daemon failed: %v", err)
		}
	}()
	status, err := daemonClient(t, second).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if len(status.Providers) != 2 || status.Providers[0].ID != "mock" || !status.Providers[0].Available {
		t.Fatalf("providers = %#v", status.Providers)
	}
	openAI := status.Providers[1]
	if openAI.ID != "openai-primary" || openAI.Available || openAI.Code != "authentication_failed" || openAI.Reference != "OPENAI_API_KEY" {
		t.Fatalf("OpenAI provider = %#v", openAI)
	}
	store, err := inferencetoken.NewStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.Load(second.paths.Instance)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, status.DataPlane.BaseURL+"/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token.String())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /models failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var document struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatalf("decode models failed: %v", err)
	}
	if response.StatusCode != http.StatusOK || len(document.Data) != 8 {
		t.Fatalf("models status/data = %d, %#v", response.StatusCode, document.Data)
	}
	for _, model := range document.Data {
		if len(model.ID) >= len("openai/") && model.ID[:len("openai/")] == "openai/" {
			t.Fatalf("unavailable OpenAI model advertised: %q", model.ID)
		}
	}
}

func TestDaemonControlOnlyStatusOmitsDataPlane(t *testing.T) {
	d, cancel, done := startDaemon(t, daemonOptions(t, t.TempDir(), shortRuntimeRoot(t)))
	status, err := daemonClient(t, d).Status(context.Background())
	if err != nil || status.DataPlane != nil {
		t.Fatalf("Status() = %#v, %v", status, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() failed: %v", err)
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
