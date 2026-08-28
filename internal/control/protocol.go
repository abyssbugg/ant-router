package control

import "time"

const APIVersion = "v1"

const JSONContentType = "application/json; charset=utf-8"

type HealthResponse struct {
	Status     string `json:"status"`
	APIVersion string `json:"api_version"`
}

type VersionResponse struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	BuiltAt    string `json:"built_at"`
	APIVersion string `json:"api_version"`
}

type ConfigSummary struct {
	Revision   string `json:"revision"`
	Generation uint64 `json:"generation"`
	Status     string `json:"status"`
}

type DataPlaneSummary struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

type StatusResponse struct {
	Instance      string            `json:"instance"`
	DaemonState   string            `json:"daemon_state"`
	StartedAt     time.Time         `json:"started_at"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	Config        *ConfigSummary    `json:"config,omitempty"`
	DataPlane     *DataPlaneSummary `json:"data_plane,omitempty"`
}
