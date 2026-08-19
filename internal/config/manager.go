package config

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abyssbugg/ant-router/internal/configstore"
)

// Manager validates and transactionally activates configuration snapshots.
type Manager struct {
	mu      sync.Mutex
	store   configstore.Store
	now     func() time.Time
	current atomic.Pointer[Snapshot]
}

func NewManager(store configstore.Store, now func() time.Time) (*Manager, error) {
	if store == nil {
		return nil, &Error{Code: CodeValidationFailed, Path: "store"}
	}
	if now == nil {
		now = time.Now
	}
	return &Manager{store: store, now: now}, nil
}

// Current returns the active snapshot without taking the mutation lock.
func (m *Manager) Current() (Snapshot, bool) {
	current := m.current.Load()
	if current == nil {
		return Snapshot{}, false
	}
	return *current, true
}

// Validate loads a candidate against the generation active at validation time.
func (m *Manager) Validate(ctx context.Context, path string) (Candidate, error) {
	if err := managerContextError(ctx); err != nil {
		return Candidate{}, err
	}
	generation := uint64(0)
	if current := m.current.Load(); current != nil {
		generation = current.generation
	}
	return LoadCandidate(path, generation)
}

// Activate commits a candidate to disk before publishing it to readers.
func (m *Manager) Activate(ctx context.Context, candidate Candidate) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := managerContextError(ctx); err != nil {
		return Snapshot{}, err
	}
	current := m.current.Load()
	generation := uint64(0)
	if current != nil {
		generation = current.generation
	}
	if candidate.BaseGeneration() != generation {
		return Snapshot{}, &Error{Code: CodeStaleCandidate, Path: "generation"}
	}
	if current != nil && candidate.RevisionID() == current.revisionID {
		return *current, nil
	}
	if err := m.store.PutRevision(ctx, candidate.RevisionID(), candidate.Canonical()); err != nil {
		return Snapshot{}, err
	}
	if err := managerContextError(ctx); err != nil {
		return Snapshot{}, err
	}

	manifest := configstore.Manifest{
		FormatVersion: configstore.ManifestFormatVersion,
		Generation:    generation + 1,
		Current:       candidate.RevisionID(),
		ActivatedAt:   m.now().UTC(),
	}
	if current != nil {
		manifest.Previous = current.revisionID
	}
	if err := m.store.WriteManifest(ctx, manifest); err != nil {
		return Snapshot{}, err
	}

	next := &Snapshot{
		config:      candidate.Config(),
		revisionID:  candidate.RevisionID(),
		sourcePath:  candidate.SourcePath(),
		generation:  manifest.Generation,
		activatedAt: manifest.ActivatedAt,
		status:      SnapshotStatusActive,
	}
	m.current.Store(next)
	return *next, nil
}

func managerContextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return &Error{Code: CodeCancelled, Err: ctx.Err()}
	default:
		return nil
	}
}
