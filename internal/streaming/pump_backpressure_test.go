package streaming

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type blockingSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSink) Send(ctx context.Context, _ canonical.Event) error {
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type blockingSource struct {
	started chan struct{}
	once    sync.Once
	closed  atomic.Int32
}

func (s *blockingSource) Recv(ctx context.Context) (canonical.Event, error) {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return canonical.Event{}, ctx.Err()
}
func (s *blockingSource) Close() error { s.closed.Add(1); return nil }

func TestPumpBackpressureStopsSourceReceive(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events}
	sink := &blockingSink{started: make(chan struct{}), release: make(chan struct{})}
	resultChannel := make(chan Result, 1)
	pump := newTestPump(t, source, sink, DefaultPumpOptions())
	go func() { resultChannel <- pump.Run(context.Background()) }()
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("sink did not start")
	}
	if got := source.received.Load(); got != 4 {
		t.Fatalf("source received %d events while sink blocked, want 4", got)
	}
	close(sink.release)
	select {
	case result := <-resultChannel:
		if result.TerminalKind() != TerminalCompleted {
			t.Fatalf("result=%#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("pump did not finish")
	}
}

func TestPumpClassifiesDownstreamStall(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events}
	sink := &blockingSink{started: make(chan struct{}), release: make(chan struct{})}
	options := DefaultPumpOptions()
	options.StallTimeout = 20 * time.Millisecond
	result := newTestPump(t, source, sink, options).Run(context.Background())
	if resultCode(result) != CodeDownstreamStalled || result.Phase() != DeliveryPostcommit || result.DeliveredEvents() != 0 {
		t.Fatalf("result=%#v code=%q", result, resultCode(result))
	}
}

func TestPumpCancellationDuringReceive(t *testing.T) {
	source := &blockingSource{started: make(chan struct{})}
	sink := &recordingSink{failAt: -1}
	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan Result, 1)
	pump := newTestPump(t, source, sink, DefaultPumpOptions())
	go func() { resultChannel <- pump.Run(ctx) }()
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("source did not start")
	}
	cancel()
	select {
	case result := <-resultChannel:
		if resultCode(result) != CodeCancelled || result.TerminalKind() != TerminalCancelled || result.Phase() != DeliveryPrecommit || source.closed.Load() != 1 {
			t.Fatalf("result=%#v code=%q closed=%d", result, resultCode(result), source.closed.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("pump did not cancel")
	}
}

func TestPumpCancellationDuringSendIsPostcommit(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events}
	sink := &blockingSink{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan Result, 1)
	pump := newTestPump(t, source, sink, DefaultPumpOptions())
	go func() { resultChannel <- pump.Run(ctx) }()
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("sink did not start")
	}
	cancel()
	select {
	case result := <-resultChannel:
		if resultCode(result) != CodeCancelled || result.TerminalKind() != TerminalCancelled || result.Phase() != DeliveryPostcommit {
			t.Fatalf("result=%#v code=%q", result, resultCode(result))
		}
	case <-time.After(time.Second):
		t.Fatal("pump did not cancel")
	}
}
