package streaming

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type scriptedSource struct {
	events     []canonical.Event
	terminal   error
	received   atomic.Int32
	closed     atomic.Int32
	closeError error
}

func (s *scriptedSource) Recv(context.Context) (canonical.Event, error) {
	index := int(s.received.Add(1)) - 1
	if index < len(s.events) {
		return s.events[index], nil
	}
	if s.terminal != nil {
		return canonical.Event{}, s.terminal
	}
	return canonical.Event{}, io.EOF
}
func (s *scriptedSource) Close() error { s.closed.Add(1); return s.closeError }

type recordingSink struct {
	mu     sync.Mutex
	events []canonical.Event
	failAt int
	err    error
}

func (s *recordingSink) Send(_ context.Context, event canonical.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failAt >= 0 && len(s.events) == s.failAt {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}
func (s *recordingSink) snapshot() []canonical.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]canonical.Event(nil), s.events...)
}

func loadGoldenEvents(t *testing.T) []canonical.Event {
	t.Helper()
	raw, err := os.ReadFile("testdata/reconstructed_response_stream.json")
	if err != nil {
		t.Fatal(err)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	events := make([]canonical.Event, len(values))
	for index, value := range values {
		events[index], err = canonical.DecodeEvent(value)
		if err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
	}
	return events
}

func newTestPump(t *testing.T, source EventSource, sink EventSink, options PumpOptions) *Pump {
	t.Helper()
	machine, err := NewMachine(DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	pump, err := NewPump(machine, source, sink, options)
	if err != nil {
		t.Fatal(err)
	}
	return pump
}

func newResponseStartedForPump(t *testing.T, responseID canonical.ResponseID) canonical.Event {
	t.Helper()
	event, err := canonical.NewResponseStartedEvent(streamEventID(t, 1), responseID, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func newFailedEventForPump(t *testing.T, responseID canonical.ResponseID, sequence uint64, failure canonical.Failure) canonical.Event {
	t.Helper()
	event, err := canonical.NewResponseFailedEvent(streamEventID(t, 2), responseID, sequence, failure, nil)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func newEmptyCompletedForPump(t *testing.T, responseID canonical.ResponseID, sequence uint64) canonical.Event {
	t.Helper()
	event, err := canonical.NewResponseCompletedEvent(streamEventID(t, 2), responseID, sequence, canonical.FinishReason("content_filter"), canonical.Usage{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func resultCode(result Result) ErrorCode { code, _ := CodeOf(result.Cause()); return code }

var errSource = errors.New("source unavailable")
var errSink = errors.New("sink unavailable")
var errClose = errors.New("close unavailable")
