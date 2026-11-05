package agentpublication

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
)

// SettingsFile is the observed pre-image of the agent-owned settings file.
type SettingsFile struct {
	Path    string
	Exists  bool
	Content []byte
}

// Manager applies publications to one agent-owned settings file under a
// cross-process lock, with a journal and pre-image capture. It is separate
// from the Codex publication stack: this manager merges into agent-owned
// files, so every write is preceded by capturing the exact pre-image for
// byte-exact restore.
type Manager struct {
	store     *Store
	settings  SettingsFile
	agent     string
	observeFn func() ([]byte, error)
	writeFn   func(string, []byte) (atomicfile.Result, error)
}

// NewManager constructs one manager for the target settings file.
func NewManager(store *Store, settings SettingsFile, agent string) (*Manager, error) {
	if store == nil || settings.Path == "" || agent == "" {
		return nil, errors.New("invalid agent publication manager")
	}
	manager := &Manager{store: store, settings: settings, agent: agent}
	manager.observeFn = manager.observe
	manager.writeFn = atomicWrite
	return manager, nil
}

// Result reports the outcome of a publication mutation.
type Result struct {
	Action  string
	Changed bool
}

// Publish merges the input into the settings file. Fail-closed: the describe
// of the pre-image happens under the lock, and the write only proceeds when
// the on-disk file still matches the observed pre-image.
func (m *Manager) Publish(ctx context.Context, input PublicationInput) (Result, error) {
	return m.mutate(ctx, input, func(original []byte) ([]byte, error) {
		return MergeGeminiSettings(original, input)
	})
}

// Disable restores the exact pre-image of the settings file from the owned
// generation, reverting the publication. A first publication whose pre-image
// was absent removes the file entirely. The pre-image comes from the store,
// never from a re-observation: the on-disk file at disable time is the
// published state, not the original.
func (m *Manager) Disable(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("invalid agent publication request")
	}
	lock, err := m.store.AcquireLock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	current, found, err := m.store.Current()
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Result{Action: "disable", Changed: false}, nil
	}
	// The generation records the settings path captured at publish time;
	// disabling against a different path would corrupt the wrong file.
	if current.SettingsPath != m.settings.Path {
		return Result{}, fmt.Errorf("%w: publication targets %s, not %s", ErrDrift, current.SettingsPath, m.settings.Path)
	}
	if err := m.store.WritePending("disable", current.ID, m.settings.Path, m.agent); err != nil {
		return Result{}, err
	}
	// Drift guard: never blind-overwrite or remove a file that is neither the
	// published state nor (for an empty pre-image) absent.
	onDisk, err := m.observeFn()
	if err != nil {
		_ = m.store.ClearPending()
		return Result{}, err
	}
	if len(onDisk) > 0 && string(onDisk) != string(current.Merged()) {
		_ = m.store.ClearPending()
		return Result{}, fmt.Errorf("%w: %s changed after publication; resolve before disabling", ErrDrift, m.settings.Path)
	}
	if len(current.Original()) == 0 {
		if len(onDisk) > 0 && string(onDisk) != string(current.Merged()) {
			_ = m.store.ClearPending()
			return Result{}, fmt.Errorf("%w: %s is not the published file", ErrDrift, m.settings.Path)
		}
		if err := os.Remove(m.settings.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = m.store.ClearPending()
			return Result{}, errors.New("agent settings removal failed")
		}
	} else if _, err := m.writeFn(m.settings.Path, current.Original()); err != nil {
		_ = m.store.ClearPending()
		return Result{}, errors.New("agent settings write failed")
	}
	if err := m.store.Retract(); err != nil {
		return Result{}, err
	}
	if err := m.store.ClearPending(); err != nil {
		return Result{}, err
	}
	return Result{Action: "disable", Changed: true}, nil
}

func (m *Manager) mutate(ctx context.Context, input PublicationInput, merge func([]byte) ([]byte, error)) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("invalid agent publication request")
	}
	lock, err := m.store.AcquireLock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()

	original, err := m.observeFn()
	if err != nil {
		return Result{}, err
	}
	merged, err := merge(original)
	if err != nil {
		return Result{}, err
	}
	generation, err := m.store.Stage(m.settings.Path, m.agent, original, merged)
	if err != nil {
		return Result{}, err
	}
	if err := m.store.WritePending("publish", generation.ID, m.settings.Path, m.agent); err != nil {
		return Result{}, err
	}
	// Re-observe: a writer that changed the file after our pre-image invalidates
	// the publication rather than being silently overwritten.
	current, err := m.observeFn()
	if err != nil {
		_ = m.store.ClearPending()
		return Result{}, err
	}
	if string(current) != string(original) {
		_ = m.store.ClearPending()
		return Result{}, fmt.Errorf("%w: %s changed during publication", ErrDrift, m.settings.Path)
	}
	if _, err := m.writeFn(m.settings.Path, merged); err != nil {
		_ = m.store.ClearPending()
		return Result{}, errors.New("agent settings write failed")
	}
	if err := m.store.Commit(generation.ID); err != nil {
		return Result{Action: "publish", Changed: true}, err
	}
	if err := m.store.ClearPending(); err != nil {
		return Result{Action: "publish", Changed: true}, err
	}
	return Result{Action: "publish", Changed: true}, nil
}

// Restore republishes the previous generation's merged bytes (the generation
// retracted by Disable), or the current generation when the file has drifted.
func (m *Manager) Restore(ctx context.Context) (Result, error) {
	lock, err := m.store.AcquireLock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	previous, found, err := m.store.Previous()
	if err != nil {
		return Result{}, err
	}
	if !found {
		current, found, err := m.store.Current()
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Result{}, errors.New("no agent publication to restore")
		}
		previous = current
	}
	if err := m.store.WritePending("restore", previous.ID, previous.SettingsPath, previous.Agent); err != nil {
		return Result{}, err
	}
	if _, err := m.writeFn(previous.SettingsPath, previous.Merged()); err != nil {
		_ = m.store.ClearPending()
		return Result{}, errors.New("agent settings write failed")
	}
	if err := m.store.Commit(previous.ID); err != nil {
		return Result{Action: "restore", Changed: true}, err
	}
	if err := m.store.ClearPending(); err != nil {
		return Result{Action: "restore", Changed: true}, err
	}
	return Result{Action: "restore", Changed: true}, nil
}

// Status reports the current publication state without mutating anything.
type Status struct {
	Published bool
	Drifted   bool
}

// Status inspects whether the on-disk settings match the owned generation.
func (m *Manager) Status() (Status, error) {
	current, found, err := m.store.Current()
	if err != nil {
		return Status{}, err
	}
	if !found {
		return Status{Published: false}, nil
	}
	onDisk, err := m.observe()
	if err != nil {
		return Status{}, err
	}
	return Status{Published: true, Drifted: string(onDisk) != string(current.Merged())}, nil
}

// observe reads the settings file with symlink and size guards, returning an
// empty slice when absent.
func (m *Manager) observe() ([]byte, error) {
	info, err := os.Lstat(m.settings.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrUnrepresentable, m.settings.Path)
	}
	if info.Size() > MaxSettingsBytes {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrUnrepresentable, m.settings.Path, MaxSettingsBytes)
	}
	content, err := os.ReadFile(m.settings.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s cannot be read", ErrUnrepresentable, m.settings.Path)
	}
	return content, nil
}

// atomicWrite performs the durable temp+fsync+rename write used everywhere
// else in the router.
func atomicWrite(path string, data []byte) (atomicfile.Result, error) {
	return atomicfile.Write(context.Background(), path, data, 0o600)
}
