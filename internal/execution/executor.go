package execution

import (
	"context"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// Executor starts one provider execution and returns its canonical event source.
type Executor interface {
	Execute(context.Context, canonical.Request) (streaming.EventSource, error)
}
