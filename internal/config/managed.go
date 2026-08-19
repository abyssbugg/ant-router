package config

import (
	"bytes"
	"context"

	"github.com/abyssbugg/ant-router/internal/configstore"
)

// LoadManagedStartup restores the active snapshot from the persisted manifest.
// It does not consult an external source file or synthesize a source error.
func (m *Manager) LoadManagedStartup(ctx context.Context) (Snapshot, error) {
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
	manifest, err := m.store.ReadManifest(ctx)
	if err != nil {
		if code, ok := configstore.CodeOf(err); ok && code == configstore.CodeManifestNotFound {
			return Snapshot{}, &Error{Code: CodeNoConfiguration, Err: err}
		}
		return Snapshot{}, err
	}
	canonical, err := m.store.ReadRevision(ctx, manifest.Current)
	if err != nil {
		return Snapshot{}, err
	}
	configuration, err := Decode(bytes.NewReader(canonical))
	if err != nil {
		return Snapshot{}, err
	}
	if err := managerContextError(ctx); err != nil {
		return Snapshot{}, err
	}
	next := &Snapshot{
		config:      configuration,
		revisionID:  manifest.Current,
		generation:  manifest.Generation,
		activatedAt: manifest.ActivatedAt,
		status:      SnapshotStatusActive,
	}
	m.current.Store(next)
	return *next, nil
}
