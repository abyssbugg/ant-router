package control

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProtocolResponses(t *testing.T) {
	health := HealthResponse{Status: "ok", APIVersion: APIVersion}
	if health.APIVersion != "v1" {
		t.Errorf("APIVersion = %q, want v1", health.APIVersion)
	}
	status := StatusResponse{
		Instance:      "default",
		DaemonState:   "running",
		StartedAt:     time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
		UptimeSeconds: 12,
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if _, present := decoded["config"]; present {
		t.Error("config must be omitted when unavailable")
	}
	status.Config = &ConfigSummary{Revision: "abc", Generation: 2, Status: "active"}
	data, _ = json.Marshal(status)
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(config) failed: %v", err)
	}
	if _, present := decoded["config"]; !present {
		t.Error("config summary was omitted")
	}
}
