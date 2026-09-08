//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodexReplacementAlphaWorkflow(t *testing.T) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("Codex CLI is not installed")
	}
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
	instance := "codex-e2e"
	mainConfig := filepath.Join(codexHome, "config.toml")
	sentinel := []byte("# user-owned sentinel\nmodel = \"gpt-5\"\n")
	if err := os.WriteFile(mainConfig, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}

	address := availableLoopbackAddress(t)
	configuration := filepath.Join(t.TempDir(), "codex.toml")
	configBytes := []byte("schema_version = 5\n[data_plane]\nenabled = true\nlisten = \"" + address + "\"\nmax_concurrent_requests = 4\nshutdown_timeout = \"2s\"\n[[providers]]\nid = \"mock\"\nkind = \"mock\"\nenabled = true\n[[providers.models]]\nalias = \"mock/codex\"\n[providers.models.codex]\nenabled = true\ndisplay_name = \"Mock Codex via Ant Router\"\ndescription = \"Deterministic Ant Router Codex test model\"\ncontext_window = 128000\neffective_context_window_percent = 90\npriority = 10\nvisibility = \"list\"\n")
	if err := os.WriteFile(configuration, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	first := startDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	waitForDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	if _, stderr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, "config", "apply", configuration); code != 0 {
		t.Fatalf("apply = %d %q", code, stderr)
	}
	first.stop()
	second := startDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	waitForDaemon(t, binaries, stateRoot, runtimeRoot, instance)
	defer second.stop()

	global := []string{"--codex-home", codexHome, "--codex-bin", codexBin, "codex"}
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
	enabled := decodeJSON(t, enableOut)
	for _, commandName := range []string{"status", "doctor"} {
		if _, stderr, commandCode := routerctl(t, binaries, stateRoot, runtimeRoot, instance, append(global, commandName)...); commandCode != 0 || len(stderr) != 0 {
			t.Fatalf("Codex %s = %d %q", commandName, commandCode, stderr)
		}
	}
	generation, ok := enabled["generation"].(string)
	if !ok || generation == "" {
		t.Fatalf("enable = %#v", enabled)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"--instance", instance, "--state-dir", stateRoot}
	if runtimeRoot != "" {
		args = append(args, "--runtime-dir", runtimeRoot)
	}
	args = append(args, "--codex-home", codexHome, "--codex-bin", codexBin, "codex", "run", "--", "exec", "--model", "mock/codex", "--skip-git-repo-check", "--sandbox", "read-only", "--color", "never", "Reply once.")
	command := exec.CommandContext(ctx, binaries.routerctl, args...)
	command.Dir = workspace
	var codexOut, codexErr bytes.Buffer
	command.Stdout, command.Stderr = &codexOut, &codexErr
	if err := command.Run(); err != nil {
		if parameter := responseErrorParameter(codexErr.String()); parameter != "" {
			t.Fatalf("Codex run failed: %v param=%q", err, parameter)
		}
		lines := strings.Split(strings.TrimSpace(codexErr.String()), "\n")
		if len(lines) > 8 {
			lines = lines[len(lines)-8:]
		}
		t.Fatalf("Codex run failed: %v stderr_tail=%q", err, strings.Join(lines, "\n"))
	}
	if !strings.Contains(codexOut.String(), "mock response") {
		t.Fatalf("Codex output = %q", codexOut.String())
	}

	disablePlanOut, disablePlanErr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, append(global, "plan", "disable")...)
	if code != 0 || len(disablePlanErr) != 0 {
		t.Fatalf("plan disable = %d %q", code, disablePlanErr)
	}
	disableProof, _ := decodeJSON(t, disablePlanOut)["plan"].(string)
	if _, stderr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, append(global, "disable", "--plan", disableProof)...); code != 0 {
		t.Fatalf("disable = %d %q", code, stderr)
	}
	profilePath := filepath.Join(codexHome, "ant-router.config.toml")
	catalogPath, _ := plan["catalog_path"].(string)
	for _, path := range []string{profilePath, catalogPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("managed file still exists: %s: %v", path, err)
		}
	}

	restorePlanOut, restorePlanErr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, append(global, "plan", "restore", "--generation", generation)...)
	if code != 0 || len(restorePlanErr) != 0 {
		t.Fatalf("plan restore = %d %q", code, restorePlanErr)
	}
	restoreProof, _ := decodeJSON(t, restorePlanOut)["plan"].(string)
	if _, stderr, code := routerctl(t, binaries, stateRoot, runtimeRoot, instance, append(global, "restore", "--generation", generation, "--plan", restoreProof)...); code != 0 {
		t.Fatalf("restore = %d %q", code, stderr)
	}
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("restored profile: %v", err)
	}
	current, err := os.ReadFile(mainConfig)
	if err != nil || !bytes.Equal(current, sentinel) {
		t.Fatalf("main config changed: %v", err)
	}

	var catalog map[string]any
	catalogBytes, err := os.ReadFile(catalogPath)
	if err != nil || json.Unmarshal(catalogBytes, &catalog) != nil {
		t.Fatalf("catalog invalid: %v", err)
	}
}

func responseErrorParameter(stderr string) string {
	const marker = `"param":"`
	start := strings.LastIndex(stderr, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.IndexByte(stderr[start:], '"')
	if end < 0 || end > 256 {
		return ""
	}
	return stderr[start : start+end]
}
