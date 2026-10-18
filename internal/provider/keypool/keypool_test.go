package keypool

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// fakeSource yields events, then err (io.EOF when nil).
type fakeSource struct {
	events []canonical.Event
	err    error
	closed bool
}

func (s *fakeSource) Recv(context.Context) (canonical.Event, error) {
	if len(s.events) > 0 {
		event := s.events[0]
		s.events = s.events[1:]
		return event, nil
	}
	if s.err != nil {
		return canonical.Event{}, s.err
	}
	return canonical.Event{}, io.EOF
}

func (s *fakeSource) Close() error { s.closed = true; return nil }

// fakeMember scripts one key's outcomes in call order.
type fakeMember struct {
	mu        sync.Mutex
	name      string
	calls     int
	execErr   []error
	streamErr []error
	sources   []*fakeSource
	available bool
}

func (m *fakeMember) Availability(context.Context) (execution.Availability, error) {
	if m.available {
		return execution.Available(), nil
	}
	return execution.Unavailable(execution.CodeAuthenticationFailed, m.name)
}

func (m *fakeMember) Execute(context.Context, string, canonical.Request) (streaming.EventSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.calls
	m.calls++
	if index < len(m.execErr) && m.execErr[index] != nil {
		return nil, m.execErr[index]
	}
	source := &fakeSource{events: []canonical.Event{{}}}
	if index < len(m.streamErr) && m.streamErr[index] != nil {
		source = &fakeSource{err: m.streamErr[index]}
	}
	m.sources = append(m.sources, source)
	return source, nil
}

func (m *fakeMember) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func newTestPool(t *testing.T, members ...*fakeMember) (*Adapter, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	adapters := make([]execution.ProviderAdapter, len(members))
	for i, member := range members {
		adapters[i] = member
	}
	pool, err := New(adapters, WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	return pool, clock
}

func rateLimited(retryAfter time.Duration) error {
	return execution.NewError(execution.CodeRateLimited, "provider.test", retryAfter, nil)
}

func drain(t *testing.T, source streaming.EventSource) int {
	t.Helper()
	count := 0
	for {
		if _, err := source.Recv(context.Background()); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Recv() = %v", err)
			}
			return count
		}
		count++
	}
}

func TestFailsOverOnRetryableExecuteError(t *testing.T) {
	a := &fakeMember{name: "a", execErr: []error{rateLimited(0)}}
	b := &fakeMember{name: "b"}
	pool, _ := newTestPool(t, a, b)
	source, err := pool.Execute(context.Background(), "m", canonical.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, source); got != 1 {
		t.Fatalf("events = %d, want 1", got)
	}
	if a.callCount() != 1 || b.callCount() != 1 {
		t.Fatalf("calls a=%d b=%d", a.callCount(), b.callCount())
	}
}

func TestFailsOverOnRetryableFirstReadAndClosesFailedSource(t *testing.T) {
	a := &fakeMember{name: "a", streamErr: []error{execution.NewError(execution.CodeUnavailable, "provider.test", 0, nil)}}
	b := &fakeMember{name: "b"}
	pool, _ := newTestPool(t, a, b)
	source, err := pool.Execute(context.Background(), "m", canonical.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if got := drain(t, source); got != 1 {
		t.Fatalf("events = %d, want 1", got)
	}
	if !a.sources[0].closed {
		t.Fatal("failed source was not closed before failover")
	}
	if err := source.Close(); err != nil || !b.sources[0].closed {
		t.Fatalf("Close() = %v, delegated=%v", err, b.sources[0].closed)
	}
}

func TestDoesNotFailOverOnRequestErrors(t *testing.T) {
	for _, code := range []execution.ErrorCode{execution.CodeInvalidRequest, execution.CodeInvalidModel, execution.CodeInternal} {
		a := &fakeMember{name: "a", execErr: []error{execution.NewError(code, "provider.test", 0, nil)}}
		b := &fakeMember{name: "b"}
		pool, _ := newTestPool(t, a, b)
		_, err := pool.Execute(context.Background(), "m", canonical.Request{})
		if execution.CodeOf(err) != code {
			t.Fatalf("%s: err = %v", code, err)
		}
		if b.callCount() != 0 {
			t.Fatalf("%s: second key was tried", code)
		}
	}
	a := &fakeMember{name: "a", execErr: []error{context.Canceled}}
	b := &fakeMember{name: "b"}
	pool, _ := newTestPool(t, a, b)
	if _, err := pool.Execute(context.Background(), "m", canonical.Request{}); !errors.Is(err, context.Canceled) || b.callCount() != 0 {
		t.Fatalf("cancellation: err=%v b=%d", err, b.callCount())
	}
}

func TestErrorAfterFirstEventIsNotRetried(t *testing.T) {
	a := &fakeMember{name: "a"}
	b := &fakeMember{name: "b"}
	pool, _ := newTestPool(t, a, b)
	source, err := pool.Execute(context.Background(), "m", canonical.Request{})
	if err != nil {
		t.Fatal(err)
	}
	a.sources[0].err = rateLimited(0)
	if _, err := source.Recv(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Recv(context.Background()); execution.CodeOf(err) != execution.CodeRateLimited {
		t.Fatalf("mid-stream err = %v, want rate_limited surfaced", err)
	}
	if b.callCount() != 0 {
		t.Fatal("failed over after an event was delivered")
	}
}

func TestRoundRobinDistributesRequests(t *testing.T) {
	a, b, c := &fakeMember{name: "a"}, &fakeMember{name: "b"}, &fakeMember{name: "c"}
	pool, _ := newTestPool(t, a, b, c)
	for i := 0; i < 6; i++ {
		source, err := pool.Execute(context.Background(), "m", canonical.Request{})
		if err != nil {
			t.Fatal(err)
		}
		_ = source.Close()
	}
	if a.callCount() != 2 || b.callCount() != 2 || c.callCount() != 2 {
		t.Fatalf("calls a=%d b=%d c=%d, want 2 each", a.callCount(), b.callCount(), c.callCount())
	}
}

func TestCoolingKeyIsSkippedUntilCooldownExpires(t *testing.T) {
	a := &fakeMember{name: "a", execErr: []error{rateLimited(10 * time.Second)}}
	b := &fakeMember{name: "b"}
	pool, clock := newTestPool(t, a, b)
	for i := 0; i < 3; i++ {
		source, err := pool.Execute(context.Background(), "m", canonical.Request{})
		if err != nil {
			t.Fatal(err)
		}
		_ = source.Close()
	}
	if a.callCount() != 1 || b.callCount() != 3 {
		t.Fatalf("during cooldown calls a=%d b=%d, want 1 and 3", a.callCount(), b.callCount())
	}
	clock.now = clock.now.Add(11 * time.Second)
	for i := 0; i < 2; i++ {
		source, err := pool.Execute(context.Background(), "m", canonical.Request{})
		if err != nil {
			t.Fatal(err)
		}
		_ = source.Close()
	}
	if a.callCount() != 2 {
		t.Fatalf("after cooldown a calls = %d, want key back in rotation", a.callCount())
	}
}

func TestAuthenticationFailureCoolsDownLonger(t *testing.T) {
	a := &fakeMember{name: "a", execErr: []error{execution.NewError(execution.CodeAuthenticationFailed, "provider.test", 0, nil)}}
	b := &fakeMember{name: "b"}
	pool, clock := newTestPool(t, a, b)
	source, err := pool.Execute(context.Background(), "m", canonical.Request{})
	if err != nil {
		t.Fatal(err)
	}
	_ = source.Close()
	clock.now = clock.now.Add(time.Minute)
	for i := 0; i < 2; i++ {
		source, err := pool.Execute(context.Background(), "m", canonical.Request{})
		if err != nil {
			t.Fatal(err)
		}
		_ = source.Close()
	}
	if a.callCount() != 1 {
		t.Fatalf("revoked key retried after 1m: calls = %d", a.callCount())
	}
}

func TestAllKeysFailingReturnsLastErrorAndAllCoolingStillTries(t *testing.T) {
	a := &fakeMember{name: "a", execErr: []error{rateLimited(time.Minute), rateLimited(0)}}
	b := &fakeMember{name: "b", execErr: []error{rateLimited(2 * time.Minute), rateLimited(0)}}
	pool, _ := newTestPool(t, a, b)
	_, err := pool.Execute(context.Background(), "m", canonical.Request{})
	if execution.CodeOf(err) != execution.CodeRateLimited {
		t.Fatalf("err = %v, want rate_limited", err)
	}
	if execution.RetryAfter(err) <= 0 {
		t.Fatalf("retry-after not preserved: %v", err)
	}
	// Every key is cooling: the pool still tries them, soonest-available first.
	_, err = pool.Execute(context.Background(), "m", canonical.Request{})
	if execution.CodeOf(err) != execution.CodeRateLimited || a.callCount() != 2 || b.callCount() != 2 {
		t.Fatalf("all-cooling attempt: err=%v a=%d b=%d", err, a.callCount(), b.callCount())
	}
}

func TestAvailabilityAnyMember(t *testing.T) {
	pool, _ := newTestPool(t, &fakeMember{name: "a"}, &fakeMember{name: "b", available: true})
	availability, err := pool.Availability(context.Background())
	if err != nil || !availability.Available() {
		t.Fatalf("availability = %+v err=%v, want available", availability, err)
	}
	none, _ := newTestPool(t, &fakeMember{name: "a"}, &fakeMember{name: "b"})
	availability, err = none.Availability(context.Background())
	if err != nil || availability.Available() {
		t.Fatalf("availability = %+v err=%v, want unavailable", availability, err)
	}
}

func TestNewRejectsInvalidMembers(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("empty pool accepted")
	}
	if _, err := New([]execution.ProviderAdapter{nil}); err == nil {
		t.Fatal("nil member accepted")
	}
}
