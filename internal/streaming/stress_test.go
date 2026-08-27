//go:build streamingstress

package streaming

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type yieldingSink struct{ delegate *recordingSink }

func (s *yieldingSink) Send(ctx context.Context, event canonical.Event) error {
	runtime.Gosched()
	return s.delegate.Send(ctx, event)
}

func TestConcurrentIndependentStreamStress(t *testing.T) {
	events := loadGoldenEvents(t)
	const workers = 256
	const rounds = 8
	errorsChannel := make(chan error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wait.Done()
			for round := 0; round < rounds; round++ {
				source := &scriptedSource{events: events}
				recorder := &recordingSink{failAt: -1}
				sink := &yieldingSink{delegate: recorder}
				machine, err := NewMachine(DefaultMachineOptions())
				if err != nil {
					errorsChannel <- err
					return
				}
				pump, err := NewPump(machine, source, sink, DefaultPumpOptions())
				if err != nil {
					errorsChannel <- err
					return
				}
				result := pump.Run(context.Background())
				if !result.Valid() || result.TerminalKind() != TerminalCompleted || result.DeliveredEvents() != len(events) || len(recorder.snapshot()) != len(events) || source.closed.Load() != 1 {
					errorsChannel <- fmt.Errorf("worker result invalid")
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}
}
