package streaming

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type panicSource struct {
	closePanic bool
	closed     atomic.Int32
}

func (*panicSource) Recv(context.Context) (canonical.Event, error) { panic("sensitive source panic") }
func (s *panicSource) Close() error {
	s.closed.Add(1)
	if s.closePanic {
		panic("sensitive close panic")
	}
	return nil
}

type panicSink struct{}

func (*panicSink) Send(context.Context, canonical.Event) error { panic("sensitive sink panic") }

func TestPumpContainsSourceReceivePanic(t *testing.T) {
	source := &panicSource{}
	result := newTestPump(t, source, &recordingSink{failAt: -1}, DefaultPumpOptions()).Run(context.Background())
	if resultCode(result) != CodeSourceFailed || result.Phase() != DeliveryPrecommit || source.closed.Load() != 1 {
		t.Fatalf("result=%#v code=%q closed=%d", result, resultCode(result), source.closed.Load())
	}
	if result.Cause() != nil && containsSensitivePanic(result.Cause().Error()) {
		t.Fatal("source panic value leaked")
	}
}

func TestPumpContainsSinkPanic(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events}
	result := newTestPump(t, source, &panicSink{}, DefaultPumpOptions()).Run(context.Background())
	if resultCode(result) != CodeSinkFailed || result.Phase() != DeliveryPostcommit || result.DeliveredEvents() != 0 || source.closed.Load() != 1 {
		t.Fatalf("result=%#v code=%q closed=%d", result, resultCode(result), source.closed.Load())
	}
	if result.Cause() != nil && containsSensitivePanic(result.Cause().Error()) {
		t.Fatal("sink panic value leaked")
	}
}

func TestPumpContainsSourceClosePanicAndPreservesResponse(t *testing.T) {
	events := loadGoldenEvents(t)
	source := &scriptedSource{events: events}
	source.closeError = nil
	wrapped := &closePanicSource{scriptedSource: source}
	result := newTestPump(t, wrapped, &recordingSink{failAt: -1}, DefaultPumpOptions()).Run(context.Background())
	if resultCode(result) != CodeCloseFailed || result.TerminalKind() != TerminalCompleted || wrapped.closed.Load() != 1 {
		t.Fatalf("result=%#v code=%q closed=%d", result, resultCode(result), wrapped.closed.Load())
	}
	if _, ok := result.Response(); !ok {
		t.Fatal("completed response lost")
	}
	if result.Cause() != nil && containsSensitivePanic(result.Cause().Error()) {
		t.Fatal("close panic value leaked")
	}
}

type closePanicSource struct {
	*scriptedSource
	closed atomic.Int32
}

func (s *closePanicSource) Close() error { s.closed.Add(1); panic("sensitive close panic") }

func containsSensitivePanic(value string) bool {
	return strings.Contains(value, "sensitive source panic") || strings.Contains(value, "sensitive sink panic") || strings.Contains(value, "sensitive close panic")
}
