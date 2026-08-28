package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

type fakeControlClient struct {
	status          control.StatusResponse
	current         control.ConfigCurrentResponse
	validation      control.ConfigValidateResponse
	mutation        control.ConfigMutationResponse
	err             error
	validated       []byte
	applied         []byte
	proof           control.ConfigValidateResponse
	closed          bool
	inferenceToken  inferencetoken.Token
	inferenceLoaded bool
}

func (f *fakeControlClient) Status(context.Context) (control.StatusResponse, error) {
	return f.status, f.err
}
func (f *fakeControlClient) CurrentConfig(context.Context) (control.ConfigCurrentResponse, error) {
	return f.current, f.err
}
func (f *fakeControlClient) ValidateConfig(_ context.Context, data []byte) (control.ConfigValidateResponse, error) {
	f.validated = append([]byte(nil), data...)
	return f.validation, f.err
}
func (f *fakeControlClient) ApplyConfig(_ context.Context, data []byte, proof control.ConfigValidateResponse) (control.ConfigMutationResponse, error) {
	f.applied = append([]byte(nil), data...)
	f.proof = proof
	return f.mutation, f.err
}
func (f *fakeControlClient) RollbackConfig(context.Context) (control.ConfigMutationResponse, error) {
	return f.mutation, f.err
}
func (f *fakeControlClient) InferenceToken(context.Context) (inferencetoken.Token, error) {
	f.inferenceLoaded = true
	return f.inferenceToken, f.err
}
func (f *fakeControlClient) CloseIdleConnections() { f.closed = true }

func TestExecuteStatusAndCurrent(t *testing.T) {
	client := &fakeControlClient{
		status:  control.StatusResponse{Instance: "default", DaemonState: "running", UptimeSeconds: 12},
		current: control.ConfigCurrentResponse{State: "awaiting_configuration"},
	}
	var stdout, stderr bytes.Buffer
	if code := execute(context.Background(), cliOptions{}, cliCommand{Name: "status"}, bytes.NewReader(nil), &stdout, &stderr, client); code != 0 || !strings.Contains(stdout.String(), "running") {
		t.Fatalf("status = %d %q %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := execute(context.Background(), cliOptions{JSON: true}, cliCommand{Name: "config", Action: "current"}, bytes.NewReader(nil), &stdout, &stderr, client); code != 0 || !strings.Contains(stdout.String(), `"schema_version":1`) || !strings.Contains(stdout.String(), "awaiting_configuration") {
		t.Fatalf("current = %d %q", code, stdout.String())
	}
}

func TestExecuteValidateApplyAndRollback(t *testing.T) {
	revision := strings.Repeat("a", 64)
	client := &fakeControlClient{
		validation: control.ConfigValidateResponse{Revision: revision, BaseGeneration: 4, CanonicalSize: 142},
		mutation:   control.ConfigMutationResponse{State: "running", Revision: revision, Generation: 5, Status: "active", ActivatedAt: time.Now().UTC()},
	}
	data := []byte("schema_version = 1\n")
	for _, test := range []struct{ action string }{{"validate"}, {"apply"}} {
		var stdout, stderr bytes.Buffer
		code := execute(context.Background(), cliOptions{}, cliCommand{Name: "config", Action: test.action, Input: "-"}, bytes.NewReader(data), &stdout, &stderr, client)
		if code != 0 {
			t.Fatalf("%s = %d stderr %q", test.action, code, stderr.String())
		}
	}
	if !bytes.Equal(client.validated, data) || !bytes.Equal(client.applied, data) || client.proof.Revision != revision || client.proof.BaseGeneration != 4 {
		t.Errorf("validate/apply bytes or proof changed")
	}
	var stdout, stderr bytes.Buffer
	if code := execute(context.Background(), cliOptions{}, cliCommand{Name: "config", Action: "rollback"}, bytes.NewReader(nil), &stdout, &stderr, client); code != 0 {
		t.Fatalf("rollback = %d %q", code, stderr.String())
	}
}

func TestExecuteErrorExitCodes(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{&control.ClientError{Kind: control.ClientErrorConnection}, ExitConnection},
		{&control.ClientError{Kind: control.ClientErrorToken}, ExitAuthentication},
		{&control.ClientError{Kind: control.ClientErrorAPI, API: control.CodeConfigurationInvalid}, ExitValidation},
		{&control.ClientError{Kind: control.ClientErrorAPI, API: control.CodeGenerationConflict}, ExitConflict},
		{&control.ClientError{Kind: control.ClientErrorAPI, API: control.CodeConfigurationUnavailable}, ExitUnavailable},
		{errors.New("unexpected"), ExitInternal},
	}
	for _, test := range tests {
		client := &fakeControlClient{err: test.err}
		var stderr bytes.Buffer
		code := execute(context.Background(), cliOptions{}, cliCommand{Name: "status"}, bytes.NewReader(nil), &bytes.Buffer{}, &stderr, client)
		if code != test.want || stderr.Len() == 0 {
			t.Errorf("error %v = code %d stderr %q, want %d", test.err, code, stderr.String(), test.want)
		}
	}
}
