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
	pending := pendingTransaction{
		SchemaVersion:       pendingSchemaVersion,
		Action:              plan.Action(),
		TargetGeneration:    staged.ID,
		BaseGeneration:      input.OwnershipGeneration,
		ProfilePath:         input.ProfilePath,
		CatalogPath:         input.CatalogPath,
		TargetProfileExists: true,
		TargetCatalogExists: true,
		BaseProfileExists:   input.CurrentProfile.Exists,
		BaseCatalogExists:   input.CurrentCatalog.Exists,
	}
	if err := manager.writePending(ctx, pending); err != nil {
		return Result{}, err
	}
	if err := manager.verifyPlannedState(ctx, input); err != nil {
		// Best-effort journal clear: if clearing fails, a later Recover
		// completes or rolls back the recorded transaction, which is safe
		// because no file was mutated after the failed verification.
		_ = manager.clearPending()
		return Result{}, err
	}
	// Residual race: a non-cooperating writer can still modify the target
	// files between verifyPlannedState and the writes below; portable file
	// systems offer no compare-and-swap replace. Recover narrows the impact by
	// refusing to roll back over content that matches neither generation.
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
	profileAtTarget := fileMatches(profileObservation.Exists, profileBytes, pending.TargetProfileExists, target.Profile)
	catalogAtTarget := fileMatches(catalogObservation.Exists, catalogBytes, pending.TargetCatalogExists, target.Catalog)
	if profileAtTarget && catalogAtTarget {
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
	// Roll back only files whose state (existence and bytes) is one this
	// transaction could have produced: its target or its base. Anything else
	// is an external edit or removal; keep the journal and report a conflict.
	if !profileAtTarget && !fileMatches(profileObservation.Exists, profileBytes, pending.BaseProfileExists, base.Profile) {
		return ErrConflict
	}
	if !catalogAtTarget && !fileMatches(catalogObservation.Exists, catalogBytes, pending.BaseCatalogExists, base.Catalog) {
		return ErrConflict
	}
	if err := manager.restoreBaseFile(ctx, base.CatalogPath, pending.BaseCatalogExists, base.Catalog); err != nil {
		return err
	}
	if err := manager.restoreBaseFile(ctx, base.ProfilePath, pending.BaseProfileExists, base.Profile); err != nil {
		return err
	}
	return manager.clearPending()
}

// fileMatches reports whether an observed file equals a recorded state: an
// absent file matches only a state recorded as absent, a present file only a
// state recorded as present with identical bytes.
func fileMatches(exists bool, observed []byte, wantExists bool, want []byte) bool {
	if exists != wantExists {
		return false
	}
	return !exists || bytesEqual(observed, want)
}

// restoreBaseFile returns one file to its recorded base state, removing it
// when the base recorded it as absent.
func (manager *Manager) restoreBaseFile(ctx context.Context, path string, exists bool, data []byte) error {
	if !exists {
		if _, err := manager.remove(ctx, path); err != nil {
			return errors.New("Codex recovery failed")
		}
		return nil
	}
	if _, err := manager.write(ctx, path, data, 0o600); err != nil {
		return errors.New("Codex recovery failed")
	}
	if err := codexownership.SecureFile(path); err != nil {
		return errors.New("Codex recovery failed")
	}
	return nil
}

func (manager *Manager) recoverDisabled(ctx context.Context, pending pendingTransaction) error {
	for _, path := range []string{pending.ProfilePath, pending.CatalogPath} {
		if _, err := manager.remove(ctx, path); err != nil {
			return errors.New("Codex recovery removal failed")
		}
	}
	return manager.clearPending()
}

// verifyPlannedState re-observes both target files immediately before
// mutation and fails when either no longer matches the plan. This narrows,
// but cannot eliminate, the window in which a non-cooperating writer could
// be overwritten.
func (manager *Manager) verifyPlannedState(ctx context.Context, input PlanInput) error {
	profileObservation, _, err := observeFile(ctx, input.ProfilePath, codexownership.MaxProfileBytes)
	if err != nil {
		return err
	}
	catalogObservation, _, err := observeFile(ctx, input.CatalogPath, codexownership.MaxCatalogBytes)
	if err != nil {
		return err
	}
	if profileObservation != input.CurrentProfile || catalogObservation != input.CurrentCatalog {
		return errors.Join(ErrConflict, errors.New("Codex integration plan is stale"))
	}
	return nil
}

func (manager *Manager) applyDisable(ctx context.Context, plan Plan) (Result, error) {
	pending := pendingTransaction{
		SchemaVersion:     pendingSchemaVersion,
		Action:            ActionDisable,
		BaseGeneration:    plan.input.OwnershipGeneration,
		ProfilePath:       plan.input.ProfilePath,
		CatalogPath:       plan.input.CatalogPath,
		BaseProfileExists: plan.input.CurrentProfile.Exists,
		BaseCatalogExists: plan.input.CurrentCatalog.Exists,
	}
	if err := manager.writePending(ctx, pending); err != nil {
		return Result{}, err
	}
	if err := manager.verifyPlannedState(ctx, plan.input); err != nil {
		_ = manager.clearPending()
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
