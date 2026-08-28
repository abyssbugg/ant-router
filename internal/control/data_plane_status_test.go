package control

import (
	"testing"
	"time"
)

func TestRuntimeStatusValidatesDataPlane(t *testing.T) {
	started := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	valid := RuntimeStatus{Instance: "default", DaemonState: "running", StartedAt: started, DataPlane: &DataPlaneSummary{Enabled: true, BaseURL: "http://127.0.0.1:8787/v1"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	invalid := []DataPlaneSummary{
		{},
		{Enabled: true},
		{Enabled: true, BaseURL: "https://127.0.0.1:8787/v1"},
		{Enabled: true, BaseURL: "http://localhost:8787/v1"},
		{Enabled: true, BaseURL: "http://0.0.0.0:8787/v1"},
		{Enabled: true, BaseURL: "http://127.0.0.1:8787"},
		{Enabled: true, BaseURL: "http://127.0.0.1:8787/v1?secret=value"},
	}
	for _, summary := range invalid {
		status := valid
		status.DataPlane = &summary
		if err := status.Validate(); err == nil {
			t.Errorf("Validate(%#v) succeeded", summary)
		}
	}
}

func TestValidateStatusResponseRejectsUnsafeDataPlane(t *testing.T) {
	response := StatusResponse{DataPlane: &DataPlaneSummary{Enabled: true, BaseURL: "http://127.0.0.1:8787/v1';echo_pwned;'"}}
	if err := validateStatusResponse(response); err == nil {
		t.Fatal("validateStatusResponse() accepted unsafe data-plane URL")
	}
}

func TestBuildStatusResponseCopiesDataPlane(t *testing.T) {
	started := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	summary := &DataPlaneSummary{Enabled: true, BaseURL: "http://127.0.0.1:8787/v1"}
	response := buildStatusResponse(RuntimeStatus{Instance: "default", DaemonState: "running", StartedAt: started, DataPlane: summary}, started.Add(time.Second))
	if response.DataPlane == nil || response.DataPlane.BaseURL != summary.BaseURL {
		t.Fatalf("DataPlane = %#v", response.DataPlane)
	}
	summary.BaseURL = "http://127.0.0.1:9999/v1"
	if response.DataPlane.BaseURL != "http://127.0.0.1:8787/v1" {
		t.Error("response aliases runtime data-plane summary")
	}
}
