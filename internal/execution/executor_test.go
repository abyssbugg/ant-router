package execution

import (
	"context"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type testExecutor struct{}

func (testExecutor) Execute(context.Context, canonical.Request) (streaming.EventSource, error) {
	return nil, nil
}

var _ Executor = testExecutor{}
