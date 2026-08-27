package streaming

import (
	"context"
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

var errBoundaryPanic = errors.New("stream boundary panicked")

func safeRecv(source EventSource, ctx context.Context) (event canonical.Event, err error) {
	defer func() {
		if recover() != nil {
			event = canonical.Event{}
			err = errBoundaryPanic
		}
	}()
	return source.Recv(ctx)
}

func safeSend(sink EventSink, ctx context.Context, event canonical.Event) (err error) {
	defer func() {
		if recover() != nil {
			err = errBoundaryPanic
		}
	}()
	return sink.Send(ctx, event)
}

func safeClose(source EventSource) (err error) {
	defer func() {
		if recover() != nil {
			err = errBoundaryPanic
		}
	}()
	return source.Close()
}
