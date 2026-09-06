//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func waitForAnthropicDaemon(t *testing.T, binaries binaries, stateRoot, runtimeRoot, instance string, process *daemonProcess) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	lastCode := -1
	var lastError []byte
	for time.Now().Before(deadline) {
		_, stderr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "status")
		if code == 0 {
			return
		}
		lastCode, lastError = code, append(lastError[:0], stderr...)
		time.Sleep(50 * time.Millisecond)
	}
	state := "running"
	if process.command.ProcessState != nil {
		state = process.command.ProcessState.String()
	}
	t.Fatalf("daemon did not become reachable: code=%d cli=%q daemon=%q process=%s", lastCode, lastError, process.stderr.String(), state)
}

func TestAnthropicProviderSubprocessAvailability(t *testing.T) {
	const secret = "private-anthropic-e2e-key"
	t.Setenv("ANTHROPIC_API_KEY", secret)
	binaries := buildBinaries(t)
	stateRoot := t.TempDir()
	runtimeRoot := ""
	if runtime.GOOS != "windows" {
		var err error
		runtimeRoot, err = os.MkdirTemp("/tmp", "ar-a-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	}
	instance := "anthropic-e2e"
	address := availableLoopbackAddress(t)
	configuration := filepath.Join(t.TempDir(), "anthropic.toml")
	tomlBytes := []byte("schema_version = 4\n[data_plane]\nenabled = true\nlisten = \"" + address + "\"\nmax_concurrent_requests = 4\nshutdown_timeout = \"2s\"\n[[providers]]\nid = \"mock\"\nkind = \"mock\"\nenabled = true\n[[providers]]\nid = \"anthropic-primary\"\nkind = \"anthropic\"\nenabled = true\n[providers.credential]\nsource = \"env\"\nname = \"ANTHROPIC_API_KEY\"\n[[providers.models]]\nalias = \"anthropic/claude-test\"\nupstream = \"claude-test\"\ncapabilities = [\"text\"]\ndefault_max_output_tokens = 1024\nmax_output_tokens = 4096\n")
	if err := os.WriteFile(configuration, tomlBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	first := startDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	waitForAnthropicDaemon(t, binaries, stateRoot, runtimeRoot, instance, first)
	if _, stderr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "apply", configuration); code != 0 {
		t.Fatalf("config apply = %d %q", code, stderr)
	}
	first.stop()

	second := startDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	waitForAnthropicDaemon(t, binaries, stateRoot, runtimeRoot, instance, second)
	defer second.stop()
	statusOut, statusErr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "status")
	if code != 0 || bytes.Contains(statusOut, []byte(secret)) || bytes.Contains(statusErr, []byte(secret)) {
		t.Fatalf("status = code %d stdout %q stderr %q", code, statusOut, statusErr)
	}
	envOut, envErr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "data-plane", "env", "--show-token")
	if code != 0 || len(envErr) != 0 {
		t.Fatalf("env = %d %q", code, envErr)
	}
	environment := decodeJSON(t, envOut)
	baseURL, baseOK := environment["base_url"].(string)
	apiKey, keyOK := environment["api_key"].(string)
	if !baseOK || !keyOK || apiKey == "" {
		t.Fatalf("environment = %#v", environment)
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL), option.WithMaxRetries(0))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := client.Models.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, model := range models.Data {
		if model.ID == "anthropic/claude-test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("models = %#v", models.Data)
	}
}
