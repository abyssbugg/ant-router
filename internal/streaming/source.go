package streaming

import (
	"context"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

// EventSource supplies one canonical event at a time and must honor context cancellation.
type EventSource interface {
	Recv(context.Context) (canonical.Event, error)
	Close() error
}
