package mock

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type source struct {
	mu       sync.Mutex
	events   []canonical.Event
	index    int
	finalErr error
	delay    time.Duration
	closed   bool
}

func newSource(events []canonical.Event, finalErr error, delay time.Duration) *source {
	return &source{events: append([]canonical.Event(nil), events...), finalErr: finalErr, delay: delay}
}

func (s *source) Recv(ctx context.Context) (canonical.Event, error) {
	if err := ctx.Err(); err != nil {
		return canonical.Event{}, err
	}
	if s.delay > 0 {
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return canonical.Event{}, ctx.Err()
		case <-timer.C:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return canonical.Event{}, io.ErrClosedPipe
	}
	if s.index < len(s.events) {
		event := s.events[s.index]
		s.index++
		return event, nil
	}
	if s.finalErr != nil {
		err := s.finalErr
		s.finalErr = nil
		return canonical.Event{}, err
	}
	return canonical.Event{}, io.EOF
}

func (s *source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.events = nil
	s.finalErr = nil
	return nil
}

var _ streaming.EventSource = (*source)(nil)
