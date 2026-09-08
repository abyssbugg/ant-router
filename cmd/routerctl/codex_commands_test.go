package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/codexintegration"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

type fakeCodexService struct {
	plan              codexintegration.Plan
	result            codexintegration.Result
	status            codexServiceStatus
	err               error
	action            codexintegration.Action
	generation, proof string
	runArgs           []string
	runCode           int
}

func (f *fakeCodexService) Plan(_ context.Context, action codexintegration.Action, generation string) (codexintegration.Plan, error) {
	f.action, f.generation = action, generation
	return f.plan, f.err
}
func (f *fakeCodexService) Apply(_ context.Context, action codexintegration.Action, generation, proof string) (codexintegration.Result, error) {
	f.action, f.generation, f.proof = action, generation, proof
	return f.result, f.err
}
func (f *fakeCodexService) Token(context.Context) (string, error) { return "token", f.err }
func (f *fakeCodexService) Run(_ context.Context, args []string, _ io.Reader, _, _ io.Writer) (int, error) {
	f.runArgs = append([]string(nil), args...)
	return f.runCode, f.err
}
func (f *fakeCodexService) Status(context.Context) (codexServiceStatus, error) {
	return f.status, f.err
}
func (f *fakeCodexService) Doctor(context.Context) (codexServiceStatus, error) {
	return f.status, f.err
}

func testCLIPlan(t *testing.T) codexintegration.Plan {
	t.Helper()
	root := t.TempDir()
	plan, err := codexintegration.NewPlan(codexintegration.PlanInput{Action: codexintegration.ActionEnable, ProfilePath: filepath.Join(root, "ant-router.config.toml"), CatalogPath: filepath.Join(root, "models.json"), RouterctlPath: filepath.Join(root, "routerctl"), CodexVersion: "0.153.0", ConfigGeneration: 7, DesiredProfile: []byte("profile"), DesiredCatalog: []byte("catalog")})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestExecuteCodexPlanAndApply(t *testing.T) {
	plan := testCLIPlan(t)
	proof := plan.Digest()
	service := &fakeCodexService{plan: plan, result: codexintegration.Result{Action: codexintegration.ActionEnable, GenerationID: strings.Repeat("b", 64), Changed: true}}
	var stdout, stderr bytes.Buffer
	code := executeCodexService(context.Background(), cliOptions{JSON: true}, cliCommand{Name: "codex", Action: "plan-enable"}, bytes.NewReader(nil), &stdout, &stderr, service)
	if code != 0 || !strings.Contains(stdout.String(), `"plan":"`+proof+`"`) || !strings.Contains(stdout.String(), `"changes":[{"path":`) || service.action != codexintegration.ActionEnable {
		t.Fatalf("plan code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = executeCodexService(context.Background(), cliOptions{}, cliCommand{Name: "codex", Action: "enable", Proof: proof}, bytes.NewReader(nil), &stdout, &stderr, service)
	if code != 0 || service.proof != proof || !strings.Contains(stdout.String(), "Restart Codex") {
		t.Fatalf("enable code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteCodexStatusDoctorAndRun(t *testing.T) {
	service := &fakeCodexService{status: codexServiceStatus{State: "enabled", ProfilePath: "/profile", CatalogPath: "/catalog", CodexVersion: "0.153.0"}, runCode: 42}
	for _, action := range []string{"status", "doctor"} {
		var stdout bytes.Buffer
		if code := executeCodexService(context.Background(), cliOptions{}, cliCommand{Name: "codex", Action: action}, bytes.NewReader(nil), &stdout, io.Discard, service); code != 0 || !strings.Contains(stdout.String(), "enabled") {
			t.Fatalf("%s code=%d output=%q", action, code, stdout.String())
		}
	}
	if code := executeCodexService(context.Background(), cliOptions{}, cliCommand{Name: "codex", Action: "run", Args: []string{"exec", "hello"}}, bytes.NewReader([]byte("stdin")), io.Discard, io.Discard, service); code != 42 || strings.Join(service.runArgs, " ") != "exec hello" {
		t.Fatalf("run code=%d args=%v", code, service.runArgs)
	}
}

func TestExecuteCodexTokenIsRawAndJSONRejected(t *testing.T) {
	token, err := inferencetoken.Parse(strings.Repeat("AQ", 21) + "E")
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeControlClient{inferenceToken: token}
	var stdout, stderr bytes.Buffer
	if code := executeCodex(context.Background(), cliOptions{}, cliCommand{Name: "codex", Action: "token"}, bytes.NewReader(nil), &stdout, &stderr, client); code != 0 || stdout.String() != token.String()+"\n" || stderr.Len() != 0 {
		t.Fatalf("token code/output = %q %q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCodex(context.Background(), cliOptions{JSON: true}, cliCommand{Name: "codex", Action: "token"}, bytes.NewReader(nil), &stdout, &stderr, client); code != ExitUsage || stdout.Len() != 0 {
		t.Fatalf("JSON token code=%d output=%q", code, stdout.String())
	}
}
