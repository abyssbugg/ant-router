package control

import "time"

const MaxConfigRequestBody int64 = 2*(1<<20) + (16 << 10)

type ConfigCurrentResponse struct {
	State       string     `json:"state"`
	Revision    string     `json:"revision,omitempty"`
	Generation  uint64     `json:"generation,omitempty"`
	Status      string     `json:"status,omitempty"`
	ActivatedAt *time.Time `json:"activated_at,omitempty"`
	Diagnostic  string     `json:"diagnostic,omitempty"`
}

type ConfigValidateRequest struct {
	TOML string `json:"toml"`
}

type ConfigValidateResponse struct {
	Revision       string `json:"revision"`
	BaseGeneration uint64 `json:"base_generation"`
	CanonicalSize  int    `json:"canonical_size"`
}

type ConfigApplyRequest struct {
	TOML               string `json:"toml"`
	ExpectedRevision   string `json:"expected_revision"`
	ExpectedGeneration uint64 `json:"expected_generation"`
}

type ConfigMutationResponse struct {
	State       string    `json:"state"`
	Revision    string    `json:"revision"`
	Generation  uint64    `json:"generation"`
	Status      string    `json:"status"`
	ActivatedAt time.Time `json:"activated_at"`
}
