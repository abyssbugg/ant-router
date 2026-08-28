package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

func TestParseDataPlaneEnv(t *testing.T) {
	for _, test := range []struct {
		args      []string
		showToken bool
	}{
		{args: []string{"data-plane", "env"}},
		{args: []string{"data-plane", "env", "--show-token"}, showToken: true},
	} {
		_, command, err := parseCLI(test.args, &bytes.Buffer{})
		if err != nil || command.Name != "data-plane" || command.Action != "env" || command.ShowToken != test.showToken {
			t.Fatalf("parseCLI(%v) = %#v, %v", test.args, command, err)
		}
	}
	for _, args := range [][]string{{"data-plane"}, {"data-plane", "env", "--bad"}, {"data-plane", "other"}} {
		if _, _, err := parseCLI(args, &bytes.Buffer{}); err == nil {
			t.Errorf("parseCLI(%v) succeeded", args)
		}
	}
}

func TestExecuteDataPlaneEnvHidesTokenByDefault(t *testing.T) {
	client := &fakeControlClient{status: control.StatusResponse{Instance: "default", DaemonState: "running", DataPlane: &control.DataPlaneSummary{Enabled: true, BaseURL: "http://127.0.0.1:8787/v1"}}, inferenceToken: inferenceToken(t)}
	var stdout, stderr bytes.Buffer
	code := execute(context.Background(), cliOptions{}, cliCommand{Name: "data-plane", Action: "env"}, bytes.NewReader(nil), &stdout, &stderr, client)
	if code != ExitSuccess || stderr.Len() != 0 || client.inferenceLoaded {
		t.Fatalf("execute = %d stderr %q loaded %t", code, stderr.String(), client.inferenceLoaded)
	}
	if stdout.String() != "export OPENAI_BASE_URL='http://127.0.0.1:8787/v1'\n# OPENAI_API_KEY hidden; rerun with --show-token\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestExecuteDataPlaneEnvShowsTokenExplicitly(t *testing.T) {
	token := inferenceToken(t)
	client := &fakeControlClient{status: control.StatusResponse{Instance: "dev", DaemonState: "running", DataPlane: &control.DataPlaneSummary{Enabled: true, BaseURL: "http://127.0.0.1:9001/v1"}}, inferenceToken: token}
	var stdout, stderr bytes.Buffer
	command := cliCommand{Name: "data-plane", Action: "env", ShowToken: true}
	if code := execute(context.Background(), cliOptions{}, command, bytes.NewReader(nil), &stdout, &stderr, client); code != ExitSuccess || !client.inferenceLoaded {
		t.Fatalf("execute = %d loaded %t stderr %q", code, client.inferenceLoaded, stderr.String())
	}
	want := "export OPENAI_BASE_URL='http://127.0.0.1:9001/v1'\nexport OPENAI_API_KEY='" + token.String() + "'\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestExecuteDataPlaneEnvJSONAndUnavailable(t *testing.T) {
	token := inferenceToken(t)
	client := &fakeControlClient{status: control.StatusResponse{Instance: "default", DaemonState: "running", DataPlane: &control.DataPlaneSummary{Enabled: true, BaseURL: "http://127.0.0.1:8787/v1"}}, inferenceToken: token}
	var stdout, stderr bytes.Buffer
	if code := execute(context.Background(), cliOptions{JSON: true}, cliCommand{Name: "data-plane", Action: "env", ShowToken: true}, bytes.NewReader(nil), &stdout, &stderr, client); code != ExitSuccess {
		t.Fatalf("JSON execute = %d stderr %q", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result["base_url"] != "http://127.0.0.1:8787/v1" || result["api_key"] != token.String() || result["token_included"] != true {
		t.Fatalf("JSON result = %#v err %v", result, err)
	}
	stdout.Reset()
	stderr.Reset()
	client.status.DataPlane = nil
	if code := execute(context.Background(), cliOptions{}, cliCommand{Name: "data-plane", Action: "env"}, bytes.NewReader(nil), &stdout, &stderr, client); code != ExitUnavailable || !strings.Contains(stderr.String(), "data plane is unavailable") {
		t.Fatalf("unavailable = %d stderr %q", code, stderr.String())
	}
}

func TestExecuteDataPlaneEnvClassifiesCancelledTokenLoad(t *testing.T) {
	for _, tokenErr := range []error{context.Canceled, context.DeadlineExceeded} {
		client := &fakeControlClient{
			status:       control.StatusResponse{Instance: "default", DaemonState: "running", DataPlane: &control.DataPlaneSummary{Enabled: true, BaseURL: "http://127.0.0.1:8787/v1"}},
			inferenceErr: tokenErr,
		}
		var stdout, stderr bytes.Buffer
		code := execute(context.Background(), cliOptions{}, cliCommand{Name: "data-plane", Action: "env", ShowToken: true}, bytes.NewReader(nil), &stdout, &stderr, client)
		if code != ExitConnection || stdout.Len() != 0 {
			t.Fatalf("error %v = code %d stdout %q stderr %q", tokenErr, code, stdout.String(), stderr.String())
		}
	}
}

func inferenceToken(t *testing.T) inferencetoken.Token {
	t.Helper()
	token, err := inferencetoken.Parse(strings.Repeat("AQ", 21) + "E")
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	return token
}
