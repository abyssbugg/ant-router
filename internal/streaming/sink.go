package streaming

import (
	"context"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// EventSink delivers one canonical event at a time and must honor context cancellation.
type EventSink interface {
	Send(context.Context, canonical.Event) error
}
