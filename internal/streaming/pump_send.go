package streaming

import (
	"context"
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
)

func (p *Pump) send(ctx context.Context, event canonical.Event) error {
	sendContext, cancel := context.WithTimeout(ctx, p.options.StallTimeout)
	defer cancel()
	err := safeSend(p.sink, sendContext, event)
	if err == nil {
		p.delivered++
		return nil
	}
	if ctx.Err() != nil {
		return p.runtimeError(CodeCancelled, "pump.send", errInvalidTransition)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(sendContext.Err(), context.DeadlineExceeded) {
		return p.runtimeError(CodeDownstreamStalled, "pump.send", errInvalidTransition)
	}
	return p.runtimeError(CodeSinkFailed, "pump.send", errInvalidTransition)
}
