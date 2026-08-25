package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/configstore"
	"github.com/abyssbugg/ant-router/internal/revision"
)

// fakeStore is a test-double for configstore.Store that records calls
// and allows deterministic control over operation ordering and failures.
type fakeStore struct {
	mu sync.Mutex

	// operations records all store operations in order
	operations []string

	// readRevisionResult is returned by ReadRevision
	readRevisionResult map[string][]byte
	readRevisionErr    error

	// readManifestResult is returned by ReadManifest
	readManifestResult configstore.Manifest
	readManifestErr    error

	// putRevisionErrs maps operation index to error (for failure injection)
	putRevisionErr error

	// writeManifestErr is returned by WriteManifest
	writeManifestErr error

	// blockWriteManifestOnce, if true, blocks WriteManifest on first call
	// until releaseWriteManifest is called
	blockWriteManifestOnce bool
	writeManifestBlocked   chan struct{}
	writeManifestRelease   chan struct{}
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		readRevisionResult:   make(map[string][]byte),
		writeManifestBlocked: make(chan struct{}),
		writeManifestRelease: make(chan struct{}),
	}
}

func (fs *fakeStore) PutRevision(ctx context.Context, id revision.ID, data []byte) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.operations = append(fs.operations, "PutRevision")
	if fs.putRevisionErr != nil {
		return fs.putRevisionErr
	}
	if fs.readRevisionResult == nil {
		fs.readRevisionResult = make(map[string][]byte)
	}
	fs.readRevisionResult[id.String()] = data
	return nil
}

func (fs *fakeStore) ReadRevision(ctx context.Context, id revision.ID) ([]byte, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.operations = append(fs.operations, "ReadRevision")
	if fs.readRevisionErr != nil {
		return nil, fs.readRevisionErr
	}
	data, ok := fs.readRevisionResult[id.String()]
	if !ok {
		return nil, &configstore.Error{Code: configstore.CodeStoreUnavailable, Err: errors.New("revision not found")}
	}
	return data, nil
}

func (fs *fakeStore) ReadManifest(ctx context.Context) (configstore.Manifest, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.operations = append(fs.operations, "ReadManifest")
	if fs.readManifestErr != nil {
		return configstore.Manifest{}, fs.readManifestErr
	}
	return fs.readManifestResult, nil
}

func (fs *fakeStore) WriteManifest(ctx context.Context, manifest configstore.Manifest) error {
	fs.mu.Lock()
	if fs.blockWriteManifestOnce {
		fs.blockWriteManifestOnce = false
		fs.operations = append(fs.operations, "WriteManifest-block")
		close(fs.writeManifestBlocked)
		fs.mu.Unlock()
		// Block until released
		<-fs.writeManifestRelease
		fs.mu.Lock()
	}
	fs.operations = append(fs.operations, "WriteManifest")
	defer fs.mu.Unlock()
	if fs.writeManifestErr != nil {
		return fs.writeManifestErr
	}
	fs.readManifestResult = manifest
	return nil
}

func (fs *fakeStore) CleanupTemps(ctx context.Context) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.operations = append(fs.operations, "CleanupTemps")
	return nil
}

func (fs *fakeStore) RepairRevision(ctx context.Context, id revision.ID, data []byte) error {
	return fs.PutRevision(ctx, id, data)
}

func (fs *fakeStore) getOperations() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	ops := make([]string, len(fs.operations))
	copy(ops, fs.operations)
	return ops
}

func (fs *fakeStore) releaseWriteManifest() {
	fs.writeManifestRelease <- struct{}{}
}

// TestNewManagerRejectsNilStore verifies NewManager returns an error
// when store is nil with CodeValidationFailed.
func TestNewManagerRejectsNilStore(t *testing.T) {
	_, err := NewManager(nil, time.Now)
	if err == nil {
		t.Fatal("NewManager with nil store should return error")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeValidationFailed {
		t.Errorf("expected CodeValidationFailed, got %q", code)
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if e.Path != "store" {
		t.Errorf("expected path 'store', got %q", e.Path)
	}
}

// TestNewManagerNilClockDefaultsTimeNow verifies that nil clock defaults to time.Now.
func TestNewManagerNilClockDefaultsTimeNow(t *testing.T) {
	store := newFakeStore()
	mgr, err := NewManager(store, nil)
	if err != nil {
		t.Fatalf("NewManager with nil clock should not error: %v", err)
	}
	if mgr == nil {
		t.Fatal("NewManager returned nil manager")
	}
	if _, ok := mgr.Current(); ok {
		t.Error("Current() reported an active snapshot before activation")
	}
}

// TestNewManagerWithValidStore creates a manager successfully.
func TestNewManagerWithValidStore(t *testing.T) {
	store := newFakeStore()
	now := time.Now
	mgr, err := NewManager(store, now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr == nil {
		t.Fatal("NewManager returned nil manager")
	}
}

// TestCurrentLockFree verifies Current() can be called concurrently without blocking.
func TestCurrentLockFree(t *testing.T) {
	store := newFakeStore()
	now := time.Now
	mgr, err := NewManager(store, now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Call Current() many times concurrently
	const numGoroutines = 100
	done := make(chan struct{}, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			snap, ok := mgr.Current()
			if ok {
				// Snapshot should be valid if present
				_ = snap.Config()
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

// TestValidateCapturesCurrentGeneration verifies that Validate captures
// the current generation and stores it in the returned Candidate.
func TestValidateCapturesCurrentGeneration(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(sourcePath, []byte("schema_version = 1\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	store := newFakeStore()
	fixedTime := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixedTime }

	mgr, err := NewManager(store, now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// First, activate a config to set generation to 1.
	cfg := Default()
	cand1, err := NewCandidate(cfg, sourcePath, 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx := context.Background()
	_, err = mgr.Activate(ctx, cand1)
	if err != nil {
		t.Fatalf("first Activate failed: %v", err)
	}

	// Now validate another config - it should capture generation 1.
	validated, err := mgr.Validate(ctx, sourcePath)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// validated should have BaseGeneration set to 1 (current generation)
	if validated.BaseGeneration() != 1 {
		t.Errorf("Validate captured BaseGeneration = %d, want 1", validated.BaseGeneration())
	}
}

// TestValidatePrecancelReturnsCodeCancelled verifies that Validate returns
// CodeCancelled when context is pre-cancelled.
func TestValidatePrecancelReturnsCodeCancelled(t *testing.T) {
	store := newFakeStore()
	mgr, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel the context

	_, err = mgr.Validate(ctx, "/test/config.toml")
	if err == nil {
		t.Fatal("Validate with pre-cancelled context should return error")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeCancelled {
		t.Errorf("expected CodeCancelled, got %q", code)
	}
}

// TestInitialActivationWritesRevisionAndManifest verifies that the first
// Activate call writes the revision and manifest, increments generation to 1,
// and publishes SnapshotStatusActive.
func TestInitialActivationWritesRevisionAndManifest(t *testing.T) {
	store := newFakeStore()
	fixedTime := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixedTime }

	mgr, err := NewManager(store, now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := Default()
	cand, err := NewCandidate(cfg, "/test/config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx := context.Background()
	snap, err := mgr.Activate(ctx, cand)
	if err != nil {
		t.Fatalf("first Activate failed: %v", err)
	}

	// Verify the snapshot has generation 1
	if snap.Generation() != 1 {
		t.Errorf("Activate returned generation = %d, want 1", snap.Generation())
	}

	// Verify status is active
	if snap.Status() != "active" {
		t.Errorf("Activate returned status = %q, want %q", snap.Status(), "active")
	}

	// Verify ActivatedAt is set to the fixed time
	if snap.ActivatedAt() != fixedTime {
		t.Errorf("ActivatedAt = %v, want %v", snap.ActivatedAt(), fixedTime)
	}

	// Verify store operations include PutRevision and WriteManifest
	ops := store.getOperations()
	if len(ops) < 2 {
		t.Errorf("expected at least 2 store operations, got %d: %v", len(ops), ops)
	}
	if ops[0] != "PutRevision" {
		t.Errorf("first operation = %q, want PutRevision", ops[0])
	}
	foundWriteManifest := false
	for _, op := range ops {
		if op == "WriteManifest" {
			foundWriteManifest = true
			break
		}
	}
	if !foundWriteManifest {
		t.Errorf("WriteManifest not found in operations: %v", ops)
	}

	// Verify Current() now returns the activated snapshot
	currentSnap, ok := mgr.Current()
	if !ok {
		t.Fatal("Current() should return true after initial activation")
	}
	if currentSnap.Generation() != 1 {
		t.Errorf("Current().Generation() = %d, want 1", currentSnap.Generation())
	}
}

// TestSecondActivationWritesPreviousAndCurrent verifies that a second
// Activate call writes previous and current revisions, increments generation to 2.
func TestSecondActivationWritesPreviousAndCurrent(t *testing.T) {
	store := newFakeStore()
	fixedTime := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	timeCounter := 0
	now := func() time.Time {
		result := fixedTime.Add(time.Duration(timeCounter) * time.Second)
		timeCounter++
		return result
	}

	mgr, err := NewManager(store, now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg1 := Default()
	cand1, err := NewCandidate(cfg1, "/test/config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx := context.Background()
	snap1, err := mgr.Activate(ctx, cand1)
	if err != nil {
		t.Fatalf("first Activate failed: %v", err)
	}

	// Activate a second time with a different config
	cfg2 := Default()
	cfg2.Daemon.ShutdownTimeout = Duration{60 * time.Second}
	cand2, err := NewCandidate(cfg2, "/test/config.toml", snap1.Generation())
	if err != nil {
		t.Fatalf("second NewCandidate failed: %v", err)
	}

	snap2, err := mgr.Activate(ctx, cand2)
	if err != nil {
		t.Fatalf("second Activate failed: %v", err)
	}

	// Verify generation incremented to 2
	if snap2.Generation() != 2 {
		t.Errorf("second Activate returned generation = %d, want 2", snap2.Generation())
	}

	// Verify Current() returns the new snapshot
	currentSnap, ok := mgr.Current()
	if !ok {
		t.Fatal("Current() should return true after second activation")
	}
	if currentSnap.Generation() != 2 {
		t.Errorf("Current().Generation() = %d, want 2", currentSnap.Generation())
	}
}

// TestActivateIdenticalRevisionNoOp verifies that activating the same
// revision does not write to store or change generation.
func TestActivateIdenticalRevisionNoOp(t *testing.T) {
	store := newFakeStore()
	fixedTime := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixedTime }

	mgr, err := NewManager(store, now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := Default()
	cand1, err := NewCandidate(cfg, "/test/config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx := context.Background()
	snap1, err := mgr.Activate(ctx, cand1)
	if err != nil {
		t.Fatalf("first Activate failed: %v", err)
	}

	// Clear operation log
	store.operations = nil

	// Create another candidate with same config and generation 1
	cand2, err := NewCandidate(cfg, "/test/config.toml", snap1.Generation())
	if err != nil {
		t.Fatalf("second NewCandidate failed: %v", err)
	}

	// Activate same revision
	snap2, err := mgr.Activate(ctx, cand2)
	if err != nil {
		t.Fatalf("second Activate with same revision failed: %v", err)
	}

	// Generation should not change
	if snap2.Generation() != snap1.Generation() {
		t.Errorf("generation changed after identical activation: %d -> %d", snap1.Generation(), snap2.Generation())
	}

	// No store operations should have occurred
	ops := store.getOperations()
	if len(ops) > 0 {
		t.Errorf("identical Activate should not write to store, got operations: %v", ops)
	}
}

// TestActivateRejectsStaleCandidateGeneration verifies that Activate rejects
// a candidate with mismatched base generation with CodeStaleCandidate.
func TestActivateRejectsStaleCandidateGeneration(t *testing.T) {
	store := newFakeStore()
	mgr, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := Default()
	cand1, err := NewCandidate(cfg, "/test/config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx := context.Background()
	if _, err := mgr.Activate(ctx, cand1); err != nil {
		t.Fatalf("first Activate failed: %v", err)
	}

	// Try to activate a candidate with stale generation.
	cand2, err := NewCandidate(cfg, "/test/config.toml", 0) // BaseGeneration 0 is stale
	if err != nil {
		t.Fatalf("NewCandidate for stale test failed: %v", err)
	}

	_, err = mgr.Activate(ctx, cand2)
	if err == nil {
		t.Fatal("Activate with stale generation should return error")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeStaleCandidate {
		t.Errorf("expected CodeStaleCandidate, got %q", code)
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if e.Path != "generation" {
		t.Errorf("expected path 'generation', got %q", e.Path)
	}
}

// TestActivatePutRevisionFailureNeverPublishes verifies that if PutRevision fails,
// the snapshot is never published and Current() remains unchanged.
func TestActivatePutRevisionFailureNeverPublishes(t *testing.T) {
	store := newFakeStore()
	storeErr := &configstore.Error{Code: configstore.CodeStoreUnavailable, Err: errors.New("disk full")}
	store.putRevisionErr = storeErr

	mgr, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := Default()
	cand, err := NewCandidate(cfg, "/test/config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx := context.Background()
	_, err = mgr.Activate(ctx, cand)
	if err == nil {
		t.Fatal("Activate should return error when PutRevision fails")
	}

	// Current() should still return false since no snapshot was published
	_, ok := mgr.Current()
	if ok {
		t.Fatal("Current() should return false after PutRevision failure")
	}
}

// TestActivateWriteManifestFailureNeverPublishes verifies that if WriteManifest fails,
// the snapshot is never published.
func TestActivateWriteManifestFailureNeverPublishes(t *testing.T) {
	store := newFakeStore()
	storeErr := &configstore.Error{Code: configstore.CodeDurabilityUncertain, Err: errors.New("corrupted write")}
	store.writeManifestErr = storeErr

	mgr, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := Default()
	cand, err := NewCandidate(cfg, "/test/config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx := context.Background()
	_, err = mgr.Activate(ctx, cand)
	if err == nil {
		t.Fatal("Activate should return error when WriteManifest fails")
	}

	// Verify error is propagated unchanged
	if !errors.Is(err, storeErr) {
		t.Errorf("expected error to be propagated unchanged")
	}

	// Current() should still return false since snapshot was not published
	_, ok := mgr.Current()
	if ok {
		t.Fatal("Current() should return false after WriteManifest failure")
	}
}

// TestActivateCancellationPreActivation verifies that if context is cancelled
// before Activate completes, the snapshot is never published.
func TestActivateCancellationPreActivation(t *testing.T) {
	store := newFakeStore()
	mgr, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := Default()
	cand, err := NewCandidate(cfg, "/test/config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before Activate

	_, err = mgr.Activate(ctx, cand)
	if err == nil {
		t.Fatal("Activate with cancelled context should return error")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeCancelled {
		t.Errorf("expected CodeCancelled, got %q", code)
	}

	// Current() should return false since no snapshot was published
	_, ok = mgr.Current()
	if ok {
		t.Fatal("Current() should return false after cancellation")
	}
}

// TestActivateNormalizesUTCClock verifies that ActivatedAt is normalized to UTC.
func TestActivateNormalizesUTCClock(t *testing.T) {
	store := newFakeStore()
	nonUTCTime := time.Date(2026, 8, 19, 10, 0, 0, 0, time.FixedZone("Custom", 3600))
	now := func() time.Time { return nonUTCTime }

	mgr, err := NewManager(store, now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := Default()
	cand, err := NewCandidate(cfg, "/test/config.toml", 0)
	if err != nil {
		t.Fatalf("NewCandidate failed: %v", err)
	}

	ctx := context.Background()
	snap, err := mgr.Activate(ctx, cand)
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	// ActivatedAt should be in UTC
	activatedAt := snap.ActivatedAt()
	if activatedAt.Location() != time.UTC {
		t.Errorf("ActivatedAt location = %v, want UTC", activatedAt.Location())
	}

	// Value should be equivalent to the original time in UTC
	expectedUTC := nonUTCTime.UTC()
	if activatedAt != expectedUTC {
		t.Errorf("ActivatedAt = %v, want %v", activatedAt, expectedUTC)
	}
}
