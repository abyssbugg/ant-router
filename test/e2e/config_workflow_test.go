//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	openairesponses "github.com/openai/openai-go/v3/responses"
)

type binaries struct {
	routerd   string
	routerctl string
}

func buildBinaries(t *testing.T) binaries {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("filepath.Abs() failed: %v", err)
	}
	output := t.TempDir()
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	result := binaries{routerd: filepath.Join(output, "routerd"+extension), routerctl: filepath.Join(output, "routerctl"+extension)}
	builds := []struct{ command, destination string }{{"./cmd/routerd", result.routerd}, {"./cmd/routerctl", result.routerctl}}
	for _, item := range builds {
		build := exec.Command("go", "build", "-o", item.destination, item.command)
		build.Dir = root
		build.Env = append(os.Environ(), "GOTOOLCHAIN=local")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build %s failed: %v\n%s", item.command, err, output)
		}
	}
	return result
}

type daemonProcess struct {
	command  *exec.Cmd
	stderr   bytes.Buffer
	stopOnce sync.Once
}

func startDaemon(t *testing.T, binaries binaries, stateRoot, runtimeRoot, instance string) *daemonProcess {
	t.Helper()
	arguments := []string{"--instance", instance, "--state-dir", stateRoot}
	if runtimeRoot != "" {
		arguments = append(arguments, "--runtime-dir", runtimeRoot)
	}
	process := &daemonProcess{command: exec.Command(binaries.routerd, arguments...)}
	process.command.Stdout = &bytes.Buffer{}
	process.command.Stderr = &process.stderr
	if err := process.command.Start(); err != nil {
		t.Fatalf("routerd start failed: %v", err)
	}
	t.Cleanup(func() { process.stop() })
	return process
}

func (p *daemonProcess) stop() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() {
		if p.command == nil || p.command.Process == nil || p.command.ProcessState != nil {
			return
		}
		_ = p.command.Process.Kill()
		_ = p.command.Wait()
	})
}

func routerctl(t *testing.T, binaries binaries, stateRoot, runtimeRoot, instance string, arguments ...string) ([]byte, []byte, int) {
	t.Helper()
	global := []string{"--instance", instance, "--state-dir", stateRoot, "--json"}
	if runtimeRoot != "" {
		global = append(global, "--runtime-dir", runtimeRoot)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binaries.routerctl, append(global, arguments...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		return stdout.Bytes(), stderr.Bytes(), 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return stdout.Bytes(), stderr.Bytes(), exit.ExitCode()
	}
	t.Fatalf("routerctl failed to execute: %v", err)
	return nil, nil, -1
}

func waitForDaemon(t *testing.T, binaries binaries, stateRoot, runtimeRoot, instance string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, _, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "status")
		if code == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("daemon did not become reachable")
}

func decodeJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("json.Unmarshal(%q) failed: %v", data, err)
	}
	return value
}

func TestMockResponsesSubprocessWorkflow(t *testing.T) {
	binaries := buildBinaries(t)
	stateRoot := t.TempDir()
	runtimeRoot := ""
	if runtime.GOOS != "windows" {
		var err error
		runtimeRoot, err = os.MkdirTemp(os.TempDir(), "ar-sdk-e2e-")
		if err != nil {
			t.Fatalf("os.MkdirTemp() failed: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	}
	instance := "sdk-e2e"
	address := availableLoopbackAddress(t)
	configuration := filepath.Join(t.TempDir(), "inference.toml")
	tomlBytes := []byte("schema_version = 2\n[data_plane]\nenabled = true\nlisten = \"" + address + "\"\nmax_concurrent_requests = 4\nshutdown_timeout = \"2s\"\n[mock_provider]\nenabled = true\n")
	if err := os.WriteFile(configuration, tomlBytes, 0o600); err != nil {
		t.Fatalf("write inference config: %v", err)
	}

	first := startDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	waitForDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	if _, _, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "apply", configuration); code != 0 {
		t.Fatalf("config apply exit = %d", code)
	}
	if _, _, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "data-plane", "env", "--show-token"); code != 7 {
		t.Fatalf("pre-restart data-plane env exit = %d", code)
	}
	first.stop()

	second := startDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	waitForDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	defer second.stop()
	envOutput, envError, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "data-plane", "env", "--show-token")
	if code != 0 || len(envError) != 0 {
		t.Fatalf("data-plane env = code %d stdout %q stderr %q", code, envOutput, envError)
	}
	environment := decodeJSON(t, envOutput)
	baseURL, baseOK := environment["base_url"].(string)
	apiKey, keyOK := environment["api_key"].(string)
	if !baseOK || !keyOK || baseURL != "http://"+address+"/v1" || apiKey == "" || environment["token_included"] != true {
		t.Fatalf("data-plane environment = %#v", environment)
	}
	controlToken, err := os.ReadFile(filepath.Join(stateRoot, "instances", instance, "control.token"))
	if err != nil {
		t.Fatalf("read control token: %v", err)
	}
	if bytes.Contains(controlToken, []byte(apiKey)) {
		t.Fatal("control and inference tokens are not separated")
	}

	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))
	response, err := client.Responses.New(context.Background(), openairesponses.ResponseNewParams{
		Model: "mock/text",
		Input: openairesponses.ResponseNewParamsInputUnion{OfString: param.NewOpt("hello")},
	})
	if err != nil {
		t.Fatalf("official SDK response failed: %v", err)
	}
	if response == nil {
		t.Fatal("official SDK returned a nil response without an error")
	}
	if response.OutputText() != "mock response" {
		t.Fatalf("official SDK response text = %q", response.OutputText())
	}

	image := openairesponses.ResponseInputContentParamOfInputImage(openairesponses.ResponseInputImageDetail("high"))
	image.OfInputImage.ImageURL = param.NewOpt("https://secret.example/private/image?token=private")
	visionContext, cancelVision := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelVision()
	vision, err := client.Responses.New(visionContext, openairesponses.ResponseNewParams{
		Model: "mock/vision",
		Input: openairesponses.ResponseNewParamsInputUnion{OfInputItemList: openairesponses.ResponseInputParam{
			openairesponses.ResponseInputItemParamOfMessage(
				openairesponses.ResponseInputMessageContentListParam{
					openairesponses.ResponseInputContentParamOfInputText("inspect"),
					image,
				},
				openairesponses.EasyInputMessageRole("user"),
			),
		}},
	})
	if err != nil {
		t.Fatalf("official SDK vision response failed: %v", err)
	}
	if vision == nil {
		t.Fatal("official SDK returned a nil vision response without an error")
	}
	wantVision := "images=1\nimage[0]: source=remote mime=unknown detail=high dimensions=unknown"
	if vision.Model != "mock/vision" || vision.OutputText() != wantVision {
		t.Fatalf("official SDK vision response = model %q text %q", vision.Model, vision.OutputText())
	}
	if bytes.Contains([]byte(vision.OutputText()), []byte("secret.example")) || bytes.Contains([]byte(vision.OutputText()), []byte("token=private")) {
		t.Fatalf("official SDK vision response leaked image URL: %q", vision.OutputText())
	}

	request, err := http.NewRequest(http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		t.Fatalf("NewRequest() failed: %v", err)
	}
	unauthorized, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("unauthorized request failed: %v", err)
	}
	defer unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}
}

func availableLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	return address
}

func TestConfigurationWorkflow(t *testing.T) {
	binaries := buildBinaries(t)
	stateRoot := t.TempDir()
	runtimeRoot := ""
	if runtime.GOOS != "windows" {
		var err error
		runtimeRoot, err = os.MkdirTemp(os.TempDir(), "ar-e2e-")
		if err != nil {
			t.Fatalf("os.MkdirTemp() failed: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	}
	instance := "e2e"
	first := startDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	waitForDaemon(t, binaries, stateRoot, runtimeRoot, instance)

	currentOutput, currentError, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "current")
	commandOutputs := [][]byte{currentOutput}
	if code != 0 || len(currentError) != 0 || decodeJSON(t, currentOutput)["state"] != "awaiting_configuration" {
		t.Fatalf("initial current = code %d stdout %q stderr %q", code, currentOutput, currentError)
	}

	firstConfig := filepath.Join(t.TempDir(), "first.toml")
	secondConfig := filepath.Join(t.TempDir(), "second.toml")
	if err := os.WriteFile(firstConfig, []byte("schema_version = 1\n"), 0o600); err != nil {
		t.Fatalf("write first config: %v", err)
	}
	if err := os.WriteFile(secondConfig, []byte("schema_version = 1\n[daemon]\nshutdown_timeout = \"60s\"\n"), 0o600); err != nil {
		t.Fatalf("write second config: %v", err)
	}
	_, _, isolatedCode := routerctl(t, binaries, stateRoot, runtimeRoot, "other", "status")
	if isolatedCode == 0 {
		t.Fatal("different named instance reached the daemon")
	}

	for _, path := range []string{firstConfig, secondConfig} {
		validateOutput, validateError, validateCode := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "validate", path)
		commandOutputs = append(commandOutputs, validateOutput)
		if validateCode != 0 || len(validateError) != 0 || decodeJSON(t, validateOutput)["valid"] != true {
			t.Fatalf("validate %s = code %d stdout %q stderr %q", path, validateCode, validateOutput, validateError)
		}
		applyOutput, applyError, applyCode := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "apply", path)
		commandOutputs = append(commandOutputs, applyOutput)
		if applyCode != 0 || len(applyError) != 0 || decodeJSON(t, applyOutput)["state"] != "running" {
			t.Fatalf("apply %s = code %d stdout %q stderr %q", path, applyCode, applyOutput, applyError)
		}
	}
	tokenPath := filepath.Join(stateRoot, "instances", instance, "control.token")
	tokenBefore, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token before restart: %v", err)
	}
	first.stop()

	second := startDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	waitForDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	tokenAfter, err := os.ReadFile(tokenPath)
	if err != nil || !bytes.Equal(tokenBefore, tokenAfter) {
		t.Fatalf("token changed across restart: %v", err)
	}
	restoredOutput, _, restoredCode := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "current")
	restored := decodeJSON(t, restoredOutput)
	if restoredCode != 0 || restored["state"] != "running" || restored["generation"] != float64(2) {
		t.Fatalf("restored current = code %d %v", restoredCode, restored)
	}
	rollbackOutput, rollbackError, rollbackCode := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "rollback")
	rollback := decodeJSON(t, rollbackOutput)
	if rollbackCode != 0 || len(rollbackError) != 0 || rollback["generation"] != float64(3) {
		t.Fatalf("rollback = code %d stdout %q stderr %q", rollbackCode, rollbackOutput, rollbackError)
	}
	second.stop()

	commandOutputs = append(commandOutputs, restoredOutput, rollbackOutput)
	for _, output := range commandOutputs {
		for _, sensitive := range [][]byte{[]byte("shutdown_timeout"), []byte("schema_version ="), tokenBefore, []byte(stateRoot), []byte(runtimeRoot)} {
			if len(sensitive) != 0 && bytes.Contains(output, sensitive) {
				t.Fatalf("sensitive value leaked into command output: %q", sensitive)
			}
		}
	}
}
