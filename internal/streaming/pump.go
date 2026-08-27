package streaming

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync/atomic"
)

// Pump synchronously validates and delivers one canonical event stream.
// A Pump may be run once and is not otherwise safe for concurrent use.
type Pump struct {
	machine   *Machine
	source    EventSource
	sink      EventSink
	options   PumpOptions
	buffer    *precommitBuffer
	used      atomic.Bool
	phase     DeliveryPhase
	received  int
	delivered int
}

// NewPump constructs a bounded lossless pump for one stream.
func NewPump(machine *Machine, source EventSource, sink EventSink, options PumpOptions) (*Pump, error) {
	if machine == nil || machine.State() != StateAwaitingResponse {
		return nil, &Error{Code: CodeInvalidItemState, Path: "pump.machine", Phase: DeliveryNone, Err: errInvalidTransition}
	}
	if isNilInterface(source) {
		return nil, &Error{Code: CodeSourceFailed, Path: "pump.source", Phase: DeliveryNone, Err: errInvalidTransition}
	}
	if isNilInterface(sink) {
		return nil, &Error{Code: CodeSinkFailed, Path: "pump.sink", Phase: DeliveryNone, Err: errInvalidTransition}
	}
	buffer, err := newPrecommitBuffer(options)
	if err != nil {
		return nil, err
	}
	return &Pump{machine: machine, source: source, sink: sink, options: options, buffer: buffer, phase: DeliveryPrecommit}, nil
}

// Run receives, validates, and losslessly delivers the stream until a terminal outcome.
func (p *Pump) Run(ctx context.Context) Result {
	if p == nil || !p.used.CompareAndSwap(false, true) {
		return fallbackResult(DeliveryNone, p.runtimeError(CodeInvalidItemState, "pump.run", errInvalidTransition))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := p.run(ctx)
	if err := safeClose(p.source); err != nil {
		closeErr := p.runtimeError(CodeCloseFailed, "pump.source.close", errInvalidTransition)
		if result.Cause() != nil {
			closeErr = errors.Join(result.Cause(), closeErr)
		}
		result.cause = closeErr
	}
	return result
}

func (p *Pump) run(ctx context.Context) Result {
	for {
		event, err := safeRecv(p.source, ctx)
		if err != nil {
			if ctx.Err() != nil {
				return p.cancelledResult()
			}
			path := "pump.source.recv"
			if errors.Is(err, io.EOF) {
				path = "pump.source.eof"
			}
			return p.result(TerminalNone, nil, nil, nil, p.runtimeError(CodeSourceFailed, path, errInvalidTransition))
		}
		p.received++
		var transition Transition
		if p.phase == DeliveryPrecommit {
			checkpoint := p.buffer.checkpoint()
			if err := p.buffer.Append(event, isCompletedImageEvent(event)); err != nil {
				return p.result(TerminalNone, nil, nil, nil, rephase(err, DeliveryPrecommit))
			}
			transition, err = p.machine.Accept(event)
			if err != nil {
				p.buffer.restore(checkpoint)
				return p.result(TerminalNone, nil, nil, nil, rephase(err, DeliveryPrecommit))
			}
			if transition.JustCommitted() {
				p.phase = DeliveryPostcommit
				for _, buffered := range p.buffer.Drain() {
					if err := p.send(ctx, buffered); err != nil {
						return p.failureResultForSend(ctx, err)
					}
				}
			} else if transition.TerminalKind() != TerminalNone {
				return p.resultFromTransition(transition, nil)
			} else {
				continue
			}
		} else {
			transition, err = p.machine.Accept(event)
			if err != nil {
				return p.result(TerminalNone, nil, nil, nil, rephase(err, DeliveryPostcommit))
			}
			if err := p.send(ctx, event); err != nil {
				return p.failureResultForSend(ctx, err)
			}
		}
		if transition.TerminalKind() != TerminalNone {
			return p.resultFromTransition(transition, nil)
		}
	}
}

func (p *Pump) failureResultForSend(ctx context.Context, err error) Result {
	if ctx.Err() != nil || codeOfOrEmpty(err) == CodeCancelled {
		return p.cancelledResultWithCause(err)
	}
	return p.result(TerminalNone, nil, nil, nil, err)
}

func (p *Pump) runtimeError(code ErrorCode, path string, cause error) error {
	phase := DeliveryNone
	if p != nil {
		phase = p.phase
	}
	return &Error{Code: code, Path: path, Phase: phase, Err: cause}
}

func rephase(err error, phase DeliveryPhase) error {
	var streamErr *Error
	if errors.As(err, &streamErr) {
		return &Error{Code: streamErr.Code, Path: streamErr.Path, Phase: phase, Err: streamErr.Err}
	}
	return &Error{Code: CodeTerminalMismatch, Path: "pump.machine", Phase: phase, Err: errInvalidTransition}
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	}
	return false
}

func codeOfOrEmpty(err error) ErrorCode { code, _ := CodeOf(err); return code }
