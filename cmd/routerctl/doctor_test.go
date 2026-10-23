package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/service"
)

type doctorHarness struct {
	service *serviceHarness
	client  *fakeControlClient
	factErr error
	options cliOptions
}

func newDoctorHarness(t *testing.T) *doctorHarness {
	t.Helper()
	h := &doctorHarness{
		service: newServiceHarness(t),
		client: &fakeControlClient{status: control.StatusResponse{
			UptimeSeconds: 90,
			DataPlane:     &control.DataPlaneSummary{Enabled: true, BaseURL: "http://127.0.0.1:8787"},
		}},
		options: testServiceOptions(t),
	}
	h.writeToken(t, "control-token-value", 0o600)
	return h
}

func (h *doctorHarness) writeToken(t *testing.T, content string, mode os.FileMode) {
	t.Helper()
	paths, err := resolveCLIPaths(h.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.TokenPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenPath, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.TokenPath, mode); err != nil {
		t.Fatal(err)
	}
}

func (h *doctorHarness) run(t *testing.T) (int, doctorReport, string, string) {
	t.Helper()
	factory := func(cliOptions) (controlClient, error) {
		if h.factErr != nil {
			return nil, h.factErr
		}
		return h.client, nil
	}
	options := h.options
	options.JSON = true
	var stdout, stderr bytes.Buffer
	code := executeDoctor(context.Background(), options, &stdout, &stderr, factory, h.service.deps())
	var report doctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode %q: %v", stdout.String(), err)
	}
	options.JSON = false
	var text bytes.Buffer
	if textCode := executeDoctor(context.Background(), options, &text, &stderr, factory, h.service.deps()); textCode != code {
		t.Fatalf("text exit %d != json exit %d", textCode, code)
	}
	return code, report, stdout.String() + text.String(), stderr.String()
}

func checkStatus(t *testing.T, report doctorReport, name string) doctorCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("check %q missing from %+v", name, report.Checks)
	return doctorCheck{}
}

func TestDoctorHealthy(t *testing.T) {
	h := newDoctorHarness(t)
	code, report, output, stderr := h.run(t)
	if code != ExitSuccess || !report.Healthy || stderr != "" {
		t.Fatalf("code = %d report = %+v stderr = %q", code, report, stderr)
	}
	if report.SchemaVersion != cliSchemaVersion {
		t.Fatalf("schema = %d", report.SchemaVersion)
	}
	names := []string{"paths", "routerd_binary", "control_token", "service", "daemon", "data_plane"}
	if len(report.Checks) != len(names) {
		t.Fatalf("checks = %+v", report.Checks)
	}
	for index, name := range names {
		if report.Checks[index].Name != name || report.Checks[index].Status != doctorOK {
			t.Fatalf("check %d = %+v, want %s ok", index, report.Checks[index], name)
		}
	}
	if !h.client.closed {
		t.Fatal("doctor must close the control client")
	}
	if !strings.Contains(output, "[ok] data_plane: http://127.0.0.1:8787\n") {
		t.Fatalf("text output = %q", output)
	}
}

func TestDoctorDaemonUnavailableIsUnhealthy(t *testing.T) {
	h := newDoctorHarness(t)
	h.factErr = errors.New("dial failed")
	code, report, _, _ := h.run(t)
	if code != ExitUnavailable || report.Healthy {
		t.Fatalf("code = %d healthy = %v", code, report.Healthy)
	}
	if got := checkStatus(t, report, "daemon"); got.Status != doctorFail {
		t.Fatalf("daemon = %+v", got)
	}
	if got := checkStatus(t, report, "data_plane"); got.Status != doctorSkip {
		t.Fatalf("data_plane = %+v", got)
	}
}

func TestDoctorMissingTokenIsUnhealthy(t *testing.T) {
	h := newDoctorHarness(t)
	paths, _ := resolveCLIPaths(h.options)
	if err := os.Remove(paths.TokenPath); err != nil {
		t.Fatal(err)
	}
	code, report, _, _ := h.run(t)
	if code != ExitUnavailable || checkStatus(t, report, "control_token").Status != doctorFail {
		t.Fatalf("code = %d report = %+v", code, report)
	}
}

func TestDoctorWarnsOnPermissiveToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	for _, tc := range []struct {
		mode   os.FileMode
		status string
		text   string
	}{
		{0o640, doctorWarn, "0640"},
		{0o604, doctorWarn, "0604"},
		{0o644, doctorWarn, "0644"},
		{0o600, doctorOK, "token"},
	} {
		h := newDoctorHarness(t)
		h.writeToken(t, "control-token-value", tc.mode)
		code, report, _, _ := h.run(t)
		check := checkStatus(t, report, "control_token")
		if code != ExitSuccess || check.Status != tc.status || !strings.Contains(check.Message, tc.text) {
			t.Fatalf("mode %#o: code = %d check = %+v", tc.mode, code, check)
		}
	}
}

func TestDoctorServiceStatesAndPlatforms(t *testing.T) {
	cases := map[string]struct {
		configure func(*serviceHarness)
		status    string
		healthy   bool
	}{
		"stopped":       {configure: func(h *serviceHarness) { h.manager.status = service.Status{State: service.StateStopped} }, status: doctorWarn, healthy: true},
		"not installed": {configure: func(h *serviceHarness) { h.manager.status = service.Status{State: service.StateNotInstalled} }, status: doctorWarn, healthy: true},
		"unsupported":   {configure: func(h *serviceHarness) { h.managerErr = service.ErrUnsupportedPlatform{GOOS: "plan9"} }, status: doctorSkip, healthy: true},
		"status error":  {configure: func(h *serviceHarness) { h.manager.statusErr = errors.New("boom") }, status: doctorFail, healthy: false},
		"missing routerd": {
			configure: func(h *serviceHarness) { h.lookupErr = os.ErrNotExist },
			status:    doctorOK, healthy: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newDoctorHarness(t)
			tc.configure(h.service)
			_, report, _, _ := h.run(t)
			if got := checkStatus(t, report, "service"); got.Status != tc.status {
				t.Fatalf("service = %+v, want %s", got, tc.status)
			}
			if report.Healthy != tc.healthy {
				t.Fatalf("healthy = %v", report.Healthy)
			}
			if name == "missing routerd" && checkStatus(t, report, "routerd_binary").Status != doctorWarn {
				t.Fatalf("routerd_binary = %+v", checkStatus(t, report, "routerd_binary"))
			}
		})
	}
}

func TestDoctorNeverPrintsSecretsOrBackendDetail(t *testing.T) {
	const tokenSecret = "TOKEN-SECRET-c41b"
	h := newDoctorHarness(t)
	h.writeToken(t, tokenSecret, 0o600)
	h.service.manager.statusErr = errors.New(serviceSecretSentinel)
	h.service.lookupErr = errors.New(serviceSecretSentinel)
	h.factErr = errors.New(serviceSecretSentinel + tokenSecret)
	_, _, output, stderr := h.run(t)
	for _, secret := range []string{tokenSecret, serviceSecretSentinel} {
		if strings.Contains(output, secret) || strings.Contains(stderr, secret) {
			t.Fatalf("doctor leaked %q: stdout %q stderr %q", secret, output, stderr)
		}
	}

	h = newDoctorHarness(t)
	h.client.err = errors.New(serviceSecretSentinel)
	_, report, output, _ := h.run(t)
	if strings.Contains(output, serviceSecretSentinel) || checkStatus(t, report, "daemon").Status != doctorFail {
		t.Fatalf("daemon status error leaked or misreported: %q", output)
	}
}
