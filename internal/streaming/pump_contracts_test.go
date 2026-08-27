package streaming

import (
	"context"
	"io"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

type contractSource struct{}

func (*contractSource) Recv(context.Context) (canonical.Event, error) {
	return canonical.Event{}, io.EOF
}
func (*contractSource) Close() error { return nil }

type contractSink struct{}

func (*contractSink) Send(context.Context, canonical.Event) error { return nil }

var _ EventSource = (*contractSource)(nil)
var _ EventSink = (*contractSink)(nil)
