//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodexChatWireWorkflow(t *testing.T) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("Codex CLI is not installed")
	}

	// Track upstream requests for assertion.
	var upstreamMu sync.Mutex
	var upstreamReqs []upstreamRequest
	upstreamAuth := "Bearer test-deepseek-key-12345"

	// Start httptest server acting as DeepSeek-style Chat Completions upstream.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != upstreamAuth {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		upstreamMu.Lock()
		upstreamReqs = append(upstreamReqs, upstreamRequest{
			method: r.Method,
			auth:   r.Header.Get("Authorization"),
			body:   string(body),
		})
		upstreamMu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send SSE response: assistant text, stop, then usage.
		events := []string{
			`{"id":"msg-1","model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
			`{"id":"msg-1","choices":[{"index":0,"delta":{"content":"Hello from upstream"}}]}`,
			`{"id":"msg-1","choices":[{"index":0,"finish_reason":"stop","delta":{}}]}`,
			`{"id":"msg-1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
		}
		for _, event := range events {
			_, _ = io.WriteString(w, "data: "+event+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	binaries := buildBinaries(t)
	stateRoot := t.TempDir()
	codexHome := t.TempDir()
	if err := os.Chmod(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	runtimeRoot := ""
	if runtime.GOOS != "windows" {
		runtimeRoot, err = os.MkdirTemp("/tmp", "ar-cx-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	}

	instance := "codex-e2e-chat"
	address := availableLoopbackAddress(t)

	// Build config with schema_version 7, openai provider for deepseek.
	configuration := filepath.Join(t.TempDir(), "codex-chat.toml")
	configBytes := []byte(`schema_version = 7
[data_plane]
enabled = true
listen = "` + address + `"
max_concurrent_requests = 4
shutdown_timeout = "2s"

[[providers]]
id = "deepseek"
kind = "openai"
enabled = true
base_url = "` + upstream.URL + `/v1"
wire = "chat"

[providers.credential]
source = "env"
name = "OPENAI_DEEPSEEK_KEY"

[[providers.models]]
alias = "deepseek/chat"
upstream = "deepseek-chat"
capabilities = ["custom_tools","namespace_tools","parallel_tools","text","tools"]

[providers.models.codex]
enabled = true
display_name = "DeepSeek Chat"
description = "Chat Completions via DeepSeek"
context_window = 128000
effective_context_window_percent = 90
priority = 1
visibility = "list"
`)
	if err := os.WriteFile(configuration, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	// Start daemon with OPENAI_DEEPSEEK_KEY env var set.
	t.Helper()
	arguments := []string{"--instance", instance, "--state-dir", stateRoot}
	if runtimeRoot != "" {
		arguments = append(arguments, "--runtime-dir", runtimeRoot)
	}
	daemonCmd := exec.Command(binaries.routerd, arguments...)
	daemonCmd.Stdout = &bytes.Buffer{}
	daemonCmd.Stderr = &bytes.Buffer{}
	daemonCmd.Env = append(os.Environ(), "OPENAI_DEEPSEEK_KEY=test-deepseek-key-12345")
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("routerd start failed: %v", err)
	}
	daemonProc := &daemonProcess{command: daemonCmd}
	defer daemonProc.stop()
	t.Cleanup(func() { daemonProc.stop() })

	waitForDaemon(t, binaries, stateRoot, runtimeRoot, instance)

	// Apply config.
	if _, stderr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "apply", configuration); code != 0 {
		t.Fatalf("config apply = %d %q", code, stderr)
	}

	// Restart daemon to pick up config.
	daemonProc.stop()
	daemonCmd2 := exec.Command(binaries.routerd, arguments...)
	daemonCmd2.Stdout = &bytes.Buffer{}
	daemonCmd2.Stderr = &bytes.Buffer{}
	daemonCmd2.Env = append(os.Environ(), "OPENAI_DEEPSEEK_KEY=test-deepseek-key-12345")
	if err := daemonCmd2.Start(); err != nil {
		t.Fatalf("routerd restart failed: %v", err)
	}
	daemonProc2 := &daemonProcess{command: daemonCmd2}
	defer daemonProc2.stop()
	t.Cleanup(func() { daemonProc2.stop() })

	waitForDaemon(t, binaries, stateRoot, runtimeRoot, instance)

	// Enable Codex integration.
	global := []string{"--allow-untested-codex", "--codex-home", codexHome, "--codex-bin", codexBin, "codex"}
	planOut, planErr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, append(global, "plan", "enable")...)
	if code != 0 || len(planErr) != 0 {
		t.Fatalf("plan enable = %d %q", code, planErr)
	}
	plan := decodeJSON(t, planOut)
	proof, ok := plan["plan"].(string)
	if !ok || proof == "" {
		t.Fatalf("plan = %#v", plan)
	}

	enableOut, enableErr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, append(global, "enable", "--plan", proof)...)
	if code != 0 || len(enableErr) != 0 {
		t.Fatalf("enable = %d %q", code, enableErr)
	}
	_ = decodeJSON(t, enableOut)

	// Run codex exec with the routed model.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"--instance", instance, "--state-dir", stateRoot}
	if runtimeRoot != "" {
		args = append(args, "--runtime-dir", runtimeRoot)
	}
	args = append(args, "--codex-home", codexHome, "--codex-bin", codexBin, "codex", "run", "--", "exec", "--model", "deepseek/chat", "--skip-git-repo-check", "--sandbox", "read-only", "--color", "never", "Reply with 'Hello from upstream'.")
	command := exec.CommandContext(ctx, binaries.routerctl, args...)
	command.Dir = workspace
	var codexOut, codexErr bytes.Buffer
	command.Stdout, command.Stderr = &codexOut, &codexErr
	if err := command.Run(); err != nil {
		if parameter := responseErrorParameter(codexErr.String()); parameter != "" {
			t.Fatalf("Codex run failed: %v param=%q", err, parameter)
		}
		lines := strings.Split(strings.TrimSpace(codexErr.String()), "\n")
		if len(lines) > 12 {
			lines = lines[len(lines)-12:]
		}
		t.Fatalf("Codex run failed: %v stderr_tail=%q", err, strings.Join(lines, "\n"))
	}

	// Assert stdout contains upstream answer.
	if !strings.Contains(codexOut.String(), "Hello from upstream") {
		t.Fatalf("Codex output missing upstream answer: %q", codexOut.String())
	}

	// Assert on recorded upstream request.
	upstreamMu.Lock()
	recorded := append([]upstreamRequest(nil), upstreamReqs...)
	upstreamMu.Unlock()
	if len(recorded) == 0 {
		t.Fatal("No upstream request recorded")
	}
	req := recorded[len(recorded)-1]

	// Parse request body to check model, stream, tools, authorization.
	var chatReq map[string]any
	if err := json.Unmarshal([]byte(req.body), &chatReq); err != nil {
		t.Fatalf("Failed to parse upstream request body: %v\nbody=%q", err, req.body)
	}

	// Check model.
	model, ok := chatReq["model"].(string)
	if !ok || model != "deepseek-chat" {
		t.Fatalf("Expected model 'deepseek-chat', got %q", model)
	}

	// Check stream.
	stream, ok := chatReq["stream"].(bool)
	if !ok || !stream {
		t.Fatalf("Expected stream=true, got %v", stream)
	}

	// Check tools: no web_search, at least one function tool.
	tools, ok := chatReq["tools"].([]any)
	if !ok {
		t.Fatalf("Expected tools array, got %T: %v", chatReq["tools"], chatReq["tools"])
	}

	// Chat Completions accepts only function tools; Codex's hosted web_search
	// must have been withheld and its custom/namespace tools lowered.
	if len(tools) == 0 {
		t.Fatal("Upstream request has no tools")
	}
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok || toolMap["type"] != "function" {
			t.Fatalf("Upstream request has a non-function tool %v", tool)
		}
	}

	// Check authorization.
	if req.auth != upstreamAuth {
		t.Fatalf("Expected auth %q, got %q", upstreamAuth, req.auth)
	}
}

type upstreamRequest struct {
	method string
	auth   string
	body   string
}
