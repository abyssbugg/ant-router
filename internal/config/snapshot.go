package config

import "time"

// SnapshotStatus describes how a configuration snapshot became active.
type SnapshotStatus string

const (
	// SnapshotStatusActive identifies a normally activated configuration.
	SnapshotStatusActive SnapshotStatus = "active"
)

// Snapshot is an immutable, persisted configuration state.
type Snapshot struct {
	config      Config
	revisionID  RevisionID
	sourcePath  string
	generation  uint64
	activatedAt time.Time
	status      SnapshotStatus
}

func (s Snapshot) Config() Config         { return s.config }
func (s Snapshot) RevisionID() RevisionID { return s.revisionID }
func (s Snapshot) SourcePath() string     { return s.sourcePath }
func (s Snapshot) Generation() uint64     { return s.generation }
func (s Snapshot) ActivatedAt() time.Time { return s.activatedAt }
func (s Snapshot) Status() SnapshotStatus { return s.status }
