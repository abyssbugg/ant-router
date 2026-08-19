package config

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/configstore"
)

type activationResult struct {
	snapshot Snapshot
	err      error
}

func TestManagerIntegrationFileStoreActivation(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() failed: %v", err)
	}
	localTime := time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("test", 2*60*60))
	manager, err := NewManager(store, func() time.Time { return localTime })
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}

	firstCandidate, err := NewCandidate(Default(), "", 0)
	if err != nil {
		t.Fatalf("NewCandidate(first) failed: %v", err)
	}
	first, err := manager.Activate(context.Background(), firstCandidate)
	if err != nil {
		t.Fatalf("Activate(first) failed: %v", err)
	}
	if first.Generation() != 1 || first.Status() != SnapshotStatusActive {
		t.Fatalf("first snapshot generation/status = %d/%q, want 1/%q", first.Generation(), first.Status(), SnapshotStatusActive)
	}
	if first.ActivatedAt().Location() != time.UTC {
		t.Errorf("first ActivatedAt location = %v, want UTC", first.ActivatedAt().Location())
	}
	stored, err := store.ReadRevision(context.Background(), first.RevisionID())
	if err != nil {
		t.Fatalf("ReadRevision(first) failed: %v", err)
	}
	if !bytes.Equal(stored, firstCandidate.Canonical()) {
		t.Error("stored first revision differs from candidate canonical bytes")
	}

	secondConfig := Default()
	secondConfig.Daemon.ShutdownTimeout = Duration{60 * time.Second}
	secondCandidate, err := NewCandidate(secondConfig, "", first.Generation())
	if err != nil {
		t.Fatalf("NewCandidate(second) failed: %v", err)
	}
	second, err := manager.Activate(context.Background(), secondCandidate)
	if err != nil {
		t.Fatalf("Activate(second) failed: %v", err)
	}
	manifest, err := store.ReadManifest(context.Background())
	if err != nil {
		t.Fatalf("ReadManifest() failed: %v", err)
	}
	if manifest.Generation != 2 || manifest.Current != second.RevisionID() || manifest.Previous != first.RevisionID() {
		t.Errorf("manifest = generation %d, current %s, previous %s", manifest.Generation, manifest.Current, manifest.Previous)
	}
}

func TestManagerPublishesOnlyAfterManifestCommit(t *testing.T) {
	store := newFakeStore()
	manager, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}
	firstCandidate, err := NewCandidate(Default(), "", 0)
	if err != nil {
		t.Fatalf("NewCandidate(first) failed: %v", err)
	}
	first, err := manager.Activate(context.Background(), firstCandidate)
	if err != nil {
		t.Fatalf("Activate(first) failed: %v", err)
	}

	store.mu.Lock()
	store.blockWriteManifestOnce = true
	store.mu.Unlock()
	secondConfig := Default()
	secondConfig.Daemon.ShutdownTimeout = Duration{60 * time.Second}
	secondCandidate, err := NewCandidate(secondConfig, "", first.Generation())
	if err != nil {
		t.Fatalf("NewCandidate(second) failed: %v", err)
	}
	result := make(chan activationResult, 1)
	go func() {
		snapshot, activateErr := manager.Activate(context.Background(), secondCandidate)
		result <- activationResult{snapshot: snapshot, err: activateErr}
	}()
	<-store.writeManifestBlocked

	const readers = 256
	var wait sync.WaitGroup
	wait.Add(readers)
	errorsFound := make(chan error, 1)
	for range readers {
		go func() {
			defer wait.Done()
			snapshot, ok := manager.Current()
			if !ok || snapshot.Generation() != first.Generation() || snapshot.Config().Daemon.ShutdownTimeout.Duration != 30*time.Second {
				select {
				case errorsFound <- fmt.Errorf("reader observed unpublished snapshot"):
				default:
				}
			}
		}()
	}
	wait.Wait()
	select {
	case readerErr := <-errorsFound:
		t.Fatal(readerErr)
	default:
	}

	store.releaseWriteManifest()
	completed := <-result
	if completed.err != nil {
		t.Fatalf("Activate(second) failed: %v", completed.err)
	}
	if completed.snapshot.Generation() != 2 {
		t.Errorf("second generation = %d, want 2", completed.snapshot.Generation())
	}
}

func TestManagerConcurrentCandidatesOneBecomesStale(t *testing.T) {
	store := newFakeStore()
	manager, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager() failed: %v", err)
	}
	initialCandidate, err := NewCandidate(Default(), "", 0)
	if err != nil {
		t.Fatalf("NewCandidate(initial) failed: %v", err)
	}
	initial, err := manager.Activate(context.Background(), initialCandidate)
	if err != nil {
		t.Fatalf("Activate(initial) failed: %v", err)
	}

	configA := Default()
	configA.Daemon.ShutdownTimeout = Duration{45 * time.Second}
	candidateA, _ := NewCandidate(configA, "", initial.Generation())
	configB := Default()
	configB.Daemon.ShutdownTimeout = Duration{60 * time.Second}
	candidateB, _ := NewCandidate(configB, "", initial.Generation())

	store.mu.Lock()
	store.blockWriteManifestOnce = true
	store.mu.Unlock()
	results := make(chan error, 2)
	go func() {
		_, activateErr := manager.Activate(context.Background(), candidateA)
		results <- activateErr
	}()
	<-store.writeManifestBlocked
	go func() {
		_, activateErr := manager.Activate(context.Background(), candidateB)
		results <- activateErr
	}()
	store.releaseWriteManifest()

	firstErr, secondErr := <-results, <-results
	successes, stale := 0, 0
	for _, resultErr := range []error{firstErr, secondErr} {
		if resultErr == nil {
			successes++
			continue
		}
		if code, ok := CodeOf(resultErr); ok && code == CodeStaleCandidate {
			stale++
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("concurrent results: successes=%d stale=%d errors=%v/%v", successes, stale, firstErr, secondErr)
	}
}
