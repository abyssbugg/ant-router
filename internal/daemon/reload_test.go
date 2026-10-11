package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

func dataPlaneModelCount(t *testing.T, baseURL, token string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /models failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /models status = %d", response.StatusCode)
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode /models failed: %v", err)
	}
	return len(body.Data)
}

func TestConfigApplyReloadsProvidersWithoutRestart(t *testing.T) {
	stateRoot := t.TempDir()
	runtimeRoot := shortRuntimeRoot(t)
	listenConfig := net.ListenConfig{}
	dataListener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	t.Cleanup(func() { _ = dataListener.Close() })
	address := dataListener.Addr().String()
	dataPlaneSection := "[data_plane]\nenabled = true\nlisten = \"" + address + "\"\nmax_concurrent_requests = 4\nshutdown_timeout = \"2s\"\n"
	enabledConfig := []byte("schema_version = 2\n" + dataPlaneSection + "[mock_provider]\nenabled = true\n")
	disabled := config.Default()
	disabled.DataPlane.Enabled = true
	disabled.DataPlane.Listen = address
	disabled.DataPlane.MaxConcurrentRequests = 4
	disabled.DataPlane.ShutdownTimeout = config.Duration{Duration: 2_000_000_000}
	disabled.Providers = []config.ProviderConfig{{
		ID: "openai", Kind: config.ProviderKindOpenAI, Enabled: true,
		Credential: &config.CredentialRef{Source: "env", Name: "ANT_ROUTER_MISSING_TEST_CREDENTIAL"},
		Models:     []config.ProviderModelConfig{{Alias: "gpt-test", Upstream: "gpt-4o-mini", Capabilities: []string{"text"}}},
	}}
	disabledConfig, err := config.Canonical(disabled)
	if err != nil {
		t.Fatalf("Canonical(disabled) failed: %v", err)
	}

	bootstrapOptions := daemonOptions(t, stateRoot, runtimeRoot)
	bootstrap, cancelBootstrap, bootstrapDone := startDaemon(t, bootstrapOptions)
	client := daemonClient(t, bootstrap)
	proof, err := client.ValidateConfig(context.Background(), enabledConfig)
	if err != nil {
		t.Fatalf("ValidateConfig() failed: %v", err)
	}
	if _, err := client.ApplyConfig(context.Background(), enabledConfig, proof); err != nil {
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
	runningClient := daemonClient(t, d)

	store, err := inferencetoken.NewStore(stateRoot)
	if err != nil {
		t.Fatalf("NewStore() failed: %v", err)
	}
	token, err := store.Load(d.paths.Instance)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if count := dataPlaneModelCount(t, "http://"+address+"/v1", token.String()); count == 0 {
		t.Fatalf("startup /v1/models count = %d, want mock models", count)
	}

	disabledProof, err := runningClient.ValidateConfig(context.Background(), disabledConfig)
	if err != nil {
		t.Fatalf("ValidateConfig(disabled) failed: %v", err)
	}
	if _, err := runningClient.ApplyConfig(context.Background(), disabledConfig, disabledProof); err != nil {
		t.Fatalf("ApplyConfig(disabled) failed: %v", err)
	}

	if count := dataPlaneModelCount(t, "http://"+address+"/v1", token.String()); count != 0 {
		t.Fatalf("after apply /v1/models count = %d, want 0 without restart", count)
	}
	status, err := runningClient.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if len(status.Providers) != 1 || status.Providers[0].ID != "openai" || status.Providers[0].Available {
		t.Fatalf("after apply provider summaries = %#v, want the reloaded unavailable openai provider", status.Providers)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("running daemon Run() failed: %v", err)
	}
}
