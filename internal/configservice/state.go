package configservice

import (
	"time"

	"github.com/abyssbugg/ant-router/internal/config"
)

type State string

const (
	StateAwaitingConfiguration State = "awaiting_configuration"
	StateRunning               State = "running"
	StateDegraded              State = "degraded"
)

type ValidationResult struct {
	Revision       config.RevisionID
	BaseGeneration uint64
	CanonicalSize  int
}

type ApplyRequest struct {
	TOML               []byte
	ExpectedRevision   config.RevisionID
	ExpectedGeneration uint64
}

type CurrentResult struct {
	State       State
	Revision    config.RevisionID
	Generation  uint64
	Status      config.SnapshotStatus
	ActivatedAt time.Time
}
