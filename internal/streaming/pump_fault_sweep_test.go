package streaming

import (
	"context"
	"fmt"
	"testing"
)

func TestPumpSinkFailureAtEveryDeliveryPosition(t *testing.T) {
	events := loadGoldenEvents(t)
	for failAt := range events {
		t.Run(fmt.Sprintf("event_%02d", failAt), func(t *testing.T) {
			source := &scriptedSource{events: events}
			sink := &recordingSink{failAt: failAt, err: errSink}
			result := newTestPump(t, source, sink, DefaultPumpOptions()).Run(context.Background())
			expectedReceived := 4
			if failAt >= 4 {
				expectedReceived = failAt + 1
			}
			if resultCode(result) != CodeSinkFailed || result.Phase() != DeliveryPostcommit || result.DeliveredEvents() != failAt || result.ReceivedEvents() != expectedReceived || source.closed.Load() != 1 {
				t.Fatalf("result=%#v code=%q closed=%d", result, resultCode(result), source.closed.Load())
			}
		})
	}
}
