// Package keypool spreads one provider's requests across a pool of
// interchangeable credentials and fails over between them.
//
// A pool wraps one ordinary single-credential provider adapter per key, so
// it is provider-neutral: it relies only on the normalized execution error
// codes every provider already produces. Failover happens only before the
// caller has received any event. The pool calls Execute on a key and reads
// that stream's first event; an error at either point is a pre-response
// failure. After the first event is delivered, errors surface unchanged,
// because retrying could duplicate or corrupt output.
package keypool

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

const (
	// DefaultRateLimitCooldown applies to rate-limited keys without Retry-After.
	DefaultRateLimitCooldown = 30 * time.Second
	// MaxRateLimitCooldown caps upstream-supplied Retry-After delays.
	MaxRateLimitCooldown = 10 * time.Minute
	// CredentialCooldown applies to keys rejected as invalid or unauthorized.
	CredentialCooldown = 5 * time.Minute
)

// Option configures a pool.
type Option func(*Adapter)

// WithClock injects the time source used for cooldowns.
func WithClock(now func() time.Time) Option {
	return func(a *Adapter) {
		if now != nil {
			a.now = now
		}
	}
}

// Adapter is an execution.ProviderAdapter over a pool of keys.
type Adapter struct {
	members []execution.ProviderAdapter
	now     func() time.Time
	next    atomic.Uint64

	mu        sync.Mutex
	coolUntil []time.Time
}

var _ execution.ProviderAdapter = (*Adapter)(nil)

// New builds a pool from one adapter per key, in configured order.
func New(members []execution.ProviderAdapter, options ...Option) (*Adapter, error) {
	if len(members) == 0 {
		return nil, execution.NewError(execution.CodeInternal, "provider.keypool", 0, nil)
	}
	for _, member := range members {
		if member == nil {
			return nil, execution.NewError(execution.CodeInternal, "provider.keypool", 0, nil)
		}
	}
	adapter := &Adapter{
		members:   append([]execution.ProviderAdapter(nil), members...),
		now:       time.Now,
		coolUntil: make([]time.Time, len(members)),
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter, nil
}

// Availability reports available when any key is available; otherwise it
// reports the first key's unavailability.
func (a *Adapter) Availability(ctx context.Context) (execution.Availability, error) {
	var first execution.Availability
	var firstErr error
	for i, member := range a.members {
		availability, err := member.Availability(ctx)
		if err == nil && availability.Available() {
			return availability, nil
		}
		if i == 0 {
			first, firstErr = availability, err
		}
	}
	return first, firstErr
}

// Execute tries keys in round-robin order, skipping cooling keys unless every
// key is cooling, and fails over on key-scoped or transient pre-response
// failures.
func (a *Adapter) Execute(ctx context.Context, upstreamModel string, request canonical.Request) (streaming.EventSource, error) {
	var lastErr error
	for _, index := range a.order() {
		source, err := a.members[index].Execute(ctx, upstreamModel, request)
		if err == nil {
			var first canonical.Event
			first, err = source.Recv(ctx)
			if err == nil {
				return &peekedSource{first: first, source: source}, nil
			}
			_ = source.Close()
		}
		if !a.failover(index, ctx, err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// order returns key indexes for one request: ready keys from the round-robin
// start, or, when every key is cooling, all keys soonest-available first.
func (a *Adapter) order() []int {
	count := len(a.members)
	start := int(a.next.Add(1)-1) % count
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	ready := make([]int, 0, count)
	for offset := 0; offset < count; offset++ {
		index := (start + offset) % count
		if !a.coolUntil[index].After(now) {
			ready = append(ready, index)
		}
	}
	if len(ready) > 0 {
		return ready
	}
	all := make([]int, count)
	for offset := range all {
		all[offset] = (start + offset) % count
	}
	sort.SliceStable(all, func(i, j int) bool { return a.coolUntil[all[i]].Before(a.coolUntil[all[j]]) })
	return all
}

// failover classifies a pre-response failure, records any cooldown, and
// reports whether the next key should be tried.
func (a *Adapter) failover(index int, ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var cooldown time.Duration
	switch execution.CodeOf(err) {
	case execution.CodeRateLimited:
		cooldown = execution.RetryAfter(err)
		if cooldown <= 0 {
			cooldown = DefaultRateLimitCooldown
		}
		if cooldown > MaxRateLimitCooldown {
			cooldown = MaxRateLimitCooldown
		}
	case execution.CodeAuthenticationFailed, execution.CodePermissionDenied:
		cooldown = CredentialCooldown
	case execution.CodeUnavailable:
		// Transient outage or connection failure: try the next key now
		// without benching this one.
	default:
		return false
	}
	if cooldown > 0 {
		a.mu.Lock()
		a.coolUntil[index] = a.now().Add(cooldown)
		a.mu.Unlock()
	}
	return true
}

// peekedSource replays the event read during failover, then delegates.
type peekedSource struct {
	first  canonical.Event
	peeked bool
	source streaming.EventSource
}

func (s *peekedSource) Recv(ctx context.Context) (canonical.Event, error) {
	if !s.peeked {
		s.peeked = true
		return s.first, nil
	}
	return s.source.Recv(ctx)
}

func (s *peekedSource) Close() error { return s.source.Close() }
