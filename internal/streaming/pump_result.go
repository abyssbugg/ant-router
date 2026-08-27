package streaming

import (
	"github.com/abyssbugg/ant-router/internal/canonical"
)

func (p *Pump) resultFromTransition(transition Transition, cause error) Result {
	var response *canonical.Response
	if value, ok := transition.Response(); ok {
		response = &value
	}
	var failure *canonical.Failure
	if value, ok := transition.Failure(); ok {
		failure = &value
	}
	var usage *canonical.Usage
	if value, ok := transition.Usage(); ok {
		usage = &value
	}
	return p.result(transition.TerminalKind(), response, failure, usage, cause)
}

func (p *Pump) result(terminal TerminalKind, response *canonical.Response, failure *canonical.Failure, usage *canonical.Usage, cause error) Result {
	result, err := newResult(p.phase, terminal, response, failure, usage, p.received, p.delivered, p.buffer.PeakEvents(), p.buffer.PeakBytes(), cause)
	if err != nil {
		return fallbackResult(p.phase, err)
	}
	return result
}

func (p *Pump) cancelledResult() Result {
	return p.cancelledResultWithCause(p.runtimeError(CodeCancelled, "pump.context", errInvalidTransition))
}

func (p *Pump) cancelledResultWithCause(cause error) Result {
	failure, err := canonical.NewFailure(canonical.FailureParams{Code: canonical.FailureCode("cancelled"), Message: "cancelled"})
	if err != nil {
		return fallbackResult(p.phase, p.runtimeError(CodeTerminalMismatch, "pump.cancelled", errInvalidTransition))
	}
	return p.result(TerminalCancelled, nil, &failure, nil, cause)
}

func fallbackResult(phase DeliveryPhase, cause error) Result {
	result, err := newResult(phase, TerminalNone, nil, nil, nil, 0, 0, 0, 0, cause)
	if err != nil {
		return Result{phase: DeliveryNone, terminal: TerminalNone, cause: cause}
	}
	return result
}
