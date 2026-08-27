package execution

import (
	"context"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type testExecutor struct{}

func (testExecutor) Execute(context.Context, canonical.Request) (streaming.EventSource, error) {
	return nil, nil
}

func TestExecutorContractAcceptsCanonicalRequests(t *testing.T) {
	var executor Executor = testExecutor{}
	if executor == nil {
		t.Fatal("executor interface is nil")
	}
}
