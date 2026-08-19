//go:build stress

package config

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/configstore"
)

func TestManagerConcurrentReadersStress(t *testing.T) {
	store, err := configstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}
	manager, err := NewManager(store, time.Now)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	initial, err := NewCandidate(Default(), "", 0)
	if err != nil {
		t.Fatalf("NewCandidate initial failed: %v", err)
	}
	if _, err := manager.Activate(context.Background(), initial); err != nil {
		t.Fatalf("Activate initial failed: %v", err)
	}

	const readers = 64
	stop := make(chan struct{})
	errorsFound := make(chan error, 1)
	var wait sync.WaitGroup
	wait.Add(readers)
	for range readers {
		go func() {
			defer wait.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snapshot, ok := manager.Current()
				if !ok || !snapshot.RevisionID().Valid() || snapshot.Generation() == 0 {
					select {
					case errorsFound <- fmt.Errorf("reader observed invalid snapshot"):
					default:
					}
					return
				}
				if err := snapshot.Config().Validate(); err != nil {
					select {
					case errorsFound <- err:
					default:
					}
					return
				}
			}
		}()
	}

	for index := 0; index < 50; index++ {
		current, _ := manager.Current()
		cfg := Default()
		cfg.Daemon.ShutdownTimeout = Duration{time.Duration(index+2) * time.Second}
		candidate, err := NewCandidate(cfg, "", current.Generation())
		if err != nil {
			t.Fatalf("NewCandidate %d failed: %v", index, err)
		}
		if _, err := manager.Activate(context.Background(), candidate); err != nil {
			t.Fatalf("Activate %d failed: %v", index, err)
		}
	}
	close(stop)
	wait.Wait()
	select {
	case err := <-errorsFound:
		t.Fatal(err)
	default:
	}
	current, _ := manager.Current()
	if current.Generation() != 51 {
		t.Errorf("final generation = %d, want 51", current.Generation())
	}
}
