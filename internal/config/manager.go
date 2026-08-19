package config

import (
	"bytes"
	"context"
	"errors"
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

// Previous returns the previous persisted revision, when one exists.
func (m *Manager) Previous(ctx context.Context) (RevisionID, bool, error) {
	if err := managerContextError(ctx); err != nil {
		return "", false, err
	}
	manifest, err := m.store.ReadManifest(ctx)
	if err != nil {
		return "", false, err
	}
	if manifest.Previous == "" {
		return "", false, nil
	}
	return manifest.Previous, true, nil
}

// Rollback atomically activates the previous persisted revision.
func (m *Manager) Rollback(ctx context.Context) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := managerContextError(ctx); err != nil {
		return Snapshot{}, err
	}
	current := m.current.Load()
	if current == nil {
		return Snapshot{}, &Error{Code: CodeNoPreviousRevision}
	}
	manifest, err := m.store.ReadManifest(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if manifest.Current != current.revisionID || manifest.Generation != current.generation {
		return Snapshot{}, &Error{Code: CodeValidationFailed, Path: "manifest.current"}
	}
	if manifest.Previous == "" {
		return Snapshot{}, &Error{Code: CodeNoPreviousRevision}
	}
	canonical, err := m.store.ReadRevision(ctx, manifest.Previous)
	if err != nil {
		return Snapshot{}, err
	}
	previousConfig, err := Decode(bytes.NewReader(canonical))
	if err != nil {
		return Snapshot{}, err
	}
	if err := managerContextError(ctx); err != nil {
		return Snapshot{}, err
	}
	nextManifest := configstore.Manifest{
		FormatVersion: configstore.ManifestFormatVersion,
		Generation:    manifest.Generation + 1,
		Current:       manifest.Previous,
		Previous:      manifest.Current,
		ActivatedAt:   m.now().UTC(),
	}
	if err := m.store.WriteManifest(ctx, nextManifest); err != nil {
		return Snapshot{}, err
	}
	next := &Snapshot{
		config:      previousConfig,
		revisionID:  nextManifest.Current,
		sourcePath:  current.sourcePath,
		generation:  nextManifest.Generation,
		activatedAt: nextManifest.ActivatedAt,
		status:      SnapshotStatusActive,
	}
	m.current.Store(next)
	return *next, nil
}

// LoadStartup establishes the initial snapshot from source or verified stored state.
func (m *Manager) LoadStartup(ctx context.Context, sourcePath string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := managerContextError(ctx); err != nil {
		return Snapshot{}, err
	}
	if m.current.Load() != nil {
		return Snapshot{}, &Error{Code: CodeValidationFailed, Path: "startup"}
	}
	if err := m.store.CleanupTemps(ctx); err != nil {
		return Snapshot{}, err
	}

	resolvedSourcePath := sourcePath
	var sourceCandidate Candidate
	var sourceErr error
	if sourcePath != "" {
		resolvedSourcePath, sourceErr = resolveAbsolutePath(sourcePath, "source_path")
	}
	if sourceErr == nil {
		sourceCandidate, sourceErr = LoadCandidate(resolvedSourcePath, 0)
	}
	manifest, manifestErr := m.store.ReadManifest(ctx)
	manifestValid := manifestErr == nil
	if manifestValid {
		if _, err := m.store.ReadRevision(ctx, manifest.Current); err != nil {
			manifestErr = err
			manifestValid = false
		}
	}

	if sourceErr == nil {
		if manifestValid && manifest.Current == sourceCandidate.RevisionID() {
			if err := managerContextError(ctx); err != nil {
				return Snapshot{}, err
			}
			next := &Snapshot{
				config:      sourceCandidate.Config(),
				revisionID:  sourceCandidate.RevisionID(),
				sourcePath:  sourceCandidate.SourcePath(),
				generation:  manifest.Generation,
				activatedAt: manifest.ActivatedAt,
				status:      SnapshotStatusActive,
			}
			m.current.Store(next)
			return *next, nil
		}

		if err := m.store.PutRevision(ctx, sourceCandidate.RevisionID(), sourceCandidate.Canonical()); err != nil {
			return Snapshot{}, err
		}
		if err := managerContextError(ctx); err != nil {
			return Snapshot{}, err
		}
		nextManifest := configstore.Manifest{
			FormatVersion: configstore.ManifestFormatVersion,
			Generation:    1,
			Current:       sourceCandidate.RevisionID(),
			ActivatedAt:   m.now().UTC(),
		}
		if manifestValid {
			nextManifest.Generation = manifest.Generation + 1
			nextManifest.Previous = manifest.Current
		}
		if err := m.store.WriteManifest(ctx, nextManifest); err != nil {
			return Snapshot{}, err
		}
		next := &Snapshot{
			config:      sourceCandidate.Config(),
			revisionID:  sourceCandidate.RevisionID(),
			sourcePath:  sourceCandidate.SourcePath(),
			generation:  nextManifest.Generation,
			activatedAt: nextManifest.ActivatedAt,
			status:      SnapshotStatusActive,
		}
		m.current.Store(next)
		return *next, nil
	}

	if !manifestValid {
		return Snapshot{}, &Error{Code: CodeNoValidConfiguration, Err: errors.Join(sourceErr, manifestErr)}
	}
	canonical, recoveryErr := m.store.ReadRevision(ctx, manifest.Current)
	if recoveryErr != nil {
		return Snapshot{}, &Error{Code: CodeNoValidConfiguration, Err: errors.Join(sourceErr, recoveryErr)}
	}
	storedConfig, recoveryErr := Decode(bytes.NewReader(canonical))
	if recoveryErr != nil {
		return Snapshot{}, &Error{Code: CodeNoValidConfiguration, Err: errors.Join(sourceErr, recoveryErr)}
	}
	if err := managerContextError(ctx); err != nil {
		return Snapshot{}, err
	}
	next := &Snapshot{
		config:      storedConfig,
		revisionID:  manifest.Current,
		sourcePath:  resolvedSourcePath,
		generation:  manifest.Generation,
		activatedAt: manifest.ActivatedAt,
		status:      SnapshotStatusLastKnownGood,
		sourceError: sourceErr,
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
