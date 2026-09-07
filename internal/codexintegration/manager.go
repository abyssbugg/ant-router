package codexintegration

import (
	"context"
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
	"github.com/abyssbugg/ant-router/internal/codexownership"
	"github.com/abyssbugg/ant-router/internal/filelock"
)

type OwnershipStore interface {
	OwnershipSource
	Stage(context.Context, codexownership.Input) (codexownership.Generation, error)
	Commit(context.Context, string) (codexownership.Generation, error)
}

type Manager struct {
	mu        sync.Mutex
	planner   *Planner
	ownership OwnershipStore
	now       func() time.Time
	write     func(context.Context, string, []byte, os.FileMode) (atomicfile.Result, error)
	remove    func(context.Context, string) (atomicfile.Result, error)
	lockPath  string
}

type Result struct {
	Action       Action
	GenerationID string
	Changed      bool
}

func NewManager(planner *Planner, ownership OwnershipStore, now func() time.Time) (*Manager, error) {
	if planner == nil || nilInterface(ownership) || now == nil {
		return nil, errors.New("invalid Codex manager")
	}
	return &Manager{planner: planner, ownership: ownership, now: now, write: atomicfile.Write, remove: atomicfile.Remove, lockPath: filepath.Join(planner.options.StateRoot, "codex", "ant-router", "mutation.lock")}, nil
}

func (manager *Manager) Enable(ctx context.Context, proof string) (Result, error) {
	return manager.apply(ctx, ActionEnable, "", proof)
}
func (manager *Manager) Disable(ctx context.Context, proof string) (Result, error) {
	return manager.apply(ctx, ActionDisable, "", proof)
}
func (manager *Manager) Restore(ctx context.Context, generation, proof string) (Result, error) {
	return manager.apply(ctx, ActionRestore, generation, proof)
}

func (manager *Manager) apply(ctx context.Context, action Action, generation, proof string) (Result, error) {
	if manager == nil || ctx == nil || !validDigest(proof) {
		return Result{}, errors.New("invalid Codex mutation request")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	lock, err := manager.acquireLock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	var plan Plan
	var planErr error
	switch action {
	case ActionEnable:
		plan, planErr = manager.planner.PlanEnable(ctx)
	case ActionDisable:
		plan, planErr = manager.planner.PlanDisable(ctx)
	case ActionRestore:
		plan, planErr = manager.planner.PlanRestore(ctx, generation)
	default:
		return Result{}, errors.New("invalid Codex mutation request")
	}
	if planErr != nil {
		return Result{}, planErr
	}
	if subtle.ConstantTimeCompare([]byte(plan.Digest()), []byte(proof)) != 1 {
		return Result{}, errors.Join(ErrConflict, errors.New("stale Codex integration plan"))
	}
	if len(plan.Changes()) == 0 {
		return Result{Action: action, GenerationID: plan.input.OwnershipGeneration}, nil
	}
	if action == ActionDisable {
		return manager.applyDisable(ctx, plan)
	}
	return manager.applyPublication(ctx, plan)
}

func (manager *Manager) acquireLock(ctx context.Context) (*filelock.Lock, error) {
	root := filepath.Dir(manager.lockPath)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, errors.New("Codex integration lock unavailable")
	}
	if err := codexownership.SecureDirectory(root); err != nil {
		return nil, errors.New("Codex integration lock unavailable")
	}
	lock, err := filelock.Acquire(ctx, manager.lockPath)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func (manager *Manager) applyPublication(ctx context.Context, plan Plan) (Result, error) {
	input := plan.input
	staged, err := manager.ownership.Stage(ctx, codexownership.Input{ProfilePath: input.ProfilePath, CatalogPath: input.CatalogPath, Profile: input.DesiredProfile, Catalog: input.DesiredCatalog, CodexVersion: input.CodexVersion, RouterctlPath: input.RouterctlPath, CreatedAt: manager.now().UTC()})
	if err != nil {
		return Result{}, err
	}
	pending := pendingTransaction{SchemaVersion: pendingSchemaVersion, Action: plan.Action(), TargetGeneration: staged.ID, BaseGeneration: input.OwnershipGeneration, ProfilePath: input.ProfilePath, CatalogPath: input.CatalogPath}
	if err := manager.writePending(ctx, pending); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(input.CatalogPath), 0o700); err != nil {
		return Result{}, errors.New("Codex integration unavailable")
	}
	if err := codexownership.SecureDirectory(filepath.Dir(input.CatalogPath)); err != nil {
		return Result{}, errors.New("Codex integration unavailable")
	}
	if _, err := manager.write(ctx, input.CatalogPath, input.DesiredCatalog, 0o600); err != nil {
		return Result{}, errors.New("Codex integration publication failed")
	}
	if err := codexownership.SecureFile(input.CatalogPath); err != nil {
		return Result{}, errors.New("Codex integration publication failed")
	}
	if _, err := manager.write(ctx, input.ProfilePath, input.DesiredProfile, 0o600); err != nil {
		return Result{}, errors.New("Codex integration publication failed")
	}
	if err := codexownership.SecureFile(input.ProfilePath); err != nil {
		return Result{}, errors.New("Codex integration publication failed")
	}
	committed, err := manager.ownership.Commit(ctx, staged.ID)
	if err != nil {
		return Result{Action: plan.Action(), GenerationID: committed.ID, Changed: codexownership.Committed(err)}, errors.New("Codex integration ownership commit failed")
	}
	if err := manager.clearPending(); err != nil {
		return Result{Action: plan.Action(), GenerationID: committed.ID, Changed: true}, err
	}
	return Result{Action: plan.Action(), GenerationID: committed.ID, Changed: true}, nil
}

func (manager *Manager) Recover(ctx context.Context) error {
	if manager == nil || ctx == nil {
		return errors.New("invalid Codex recovery request")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	lock, err := manager.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer lock.Release()
	pending, exists, err := manager.readPending(ctx)
	if err != nil || !exists {
		return err
	}
	expectedProfile := filepath.Join(manager.planner.options.CodexHome, "ant-router.config.toml")
	expectedCatalog := filepath.Join(manager.planner.options.StateRoot, "codex", "ant-router", "models.json")
	if pending.ProfilePath != expectedProfile || pending.CatalogPath != expectedCatalog {
		return errors.New("invalid Codex pending transaction")
	}
	if pending.Action == ActionDisable {
		return manager.recoverDisabled(ctx, pending)
	}
	target, err := manager.ownership.Generation(ctx, pending.TargetGeneration)
	if err != nil {
		return errors.New("Codex pending generation unavailable")
	}
	profileObservation, profileBytes, err := observeFile(ctx, pending.ProfilePath, codexownership.MaxProfileBytes)
	if err != nil {
		return err
	}
	catalogObservation, catalogBytes, err := observeFile(ctx, pending.CatalogPath, codexownership.MaxCatalogBytes)
	if err != nil {
		return err
	}
	if profileObservation.Exists && catalogObservation.Exists && bytesEqual(profileBytes, target.Profile) && bytesEqual(catalogBytes, target.Catalog) {
		if _, err := manager.ownership.Commit(ctx, target.ID); err != nil {
			return err
		}
		return manager.clearPending()
	}
	if pending.BaseGeneration == "" {
		return manager.recoverDisabled(ctx, pending)
	}
	base, err := manager.ownership.Generation(ctx, pending.BaseGeneration)
	if err != nil || base.ProfilePath != pending.ProfilePath || base.CatalogPath != pending.CatalogPath {
		return errors.New("Codex recovery base unavailable")
	}
	if _, err := manager.write(ctx, base.CatalogPath, base.Catalog, 0o600); err != nil {
		return errors.New("Codex recovery failed")
	}
	if err := codexownership.SecureFile(base.CatalogPath); err != nil {
		return errors.New("Codex recovery failed")
	}
	if _, err := manager.write(ctx, base.ProfilePath, base.Profile, 0o600); err != nil {
		return errors.New("Codex recovery failed")
	}
	if err := codexownership.SecureFile(base.ProfilePath); err != nil {
		return errors.New("Codex recovery failed")
	}
	return manager.clearPending()
}

func (manager *Manager) recoverDisabled(ctx context.Context, pending pendingTransaction) error {
	for _, path := range []string{pending.ProfilePath, pending.CatalogPath} {
		if _, err := manager.remove(ctx, path); err != nil {
			return errors.New("Codex recovery removal failed")
		}
	}
	return manager.clearPending()
}

func (manager *Manager) applyDisable(ctx context.Context, plan Plan) (Result, error) {
	pending := pendingTransaction{SchemaVersion: pendingSchemaVersion, Action: ActionDisable, BaseGeneration: plan.input.OwnershipGeneration, ProfilePath: plan.input.ProfilePath, CatalogPath: plan.input.CatalogPath}
	if err := manager.writePending(ctx, pending); err != nil {
		return Result{}, err
	}
	for _, path := range []string{plan.input.ProfilePath, plan.input.CatalogPath} {
		if _, err := manager.remove(ctx, path); err != nil {
			return Result{Action: ActionDisable, GenerationID: plan.input.OwnershipGeneration, Changed: true}, errors.New("Codex integration removal failed")
		}
	}
	if err := manager.clearPending(); err != nil {
		return Result{Action: ActionDisable, GenerationID: plan.input.OwnershipGeneration, Changed: true}, err
	}
	return Result{Action: ActionDisable, GenerationID: plan.input.OwnershipGeneration, Changed: true}, nil
}
