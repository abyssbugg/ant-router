package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

type bufferedEvent struct {
	event canonical.Event
	bytes int
}

type bufferCheckpoint struct {
	count         int
	bytes         int
	peakEvents    int
	peakBytes     int
	oversizedUsed bool
}

type precommitBuffer struct {
	options       PumpOptions
	events        []bufferedEvent
	bytes         int
	peakEvents    int
	peakBytes     int
	oversizedUsed bool
}

func newPrecommitBuffer(options PumpOptions) (*precommitBuffer, error) {
	if !options.Valid() {
		return nil, &Error{Code: CodePrecommitLimitExceeded, Path: "pump.options", Phase: DeliveryNone, Err: errInvalidTransition}
	}
	return &precommitBuffer{options: options, events: make([]bufferedEvent, 0, min(options.MaxPrecommitEvents, 16))}, nil
}

func (b *precommitBuffer) Append(event canonical.Event, semantic bool) error {
	if b == nil || !event.Valid() {
		return &Error{Code: CodeInvalidItemState, Path: "pump.buffer.event", Phase: DeliveryPrecommit, Err: errInvalidTransition}
	}
	if len(b.events) >= b.options.MaxPrecommitEvents {
		return b.limitError("pump.buffer.events")
	}
	encoded, err := canonical.EncodeEvent(event)
	if err != nil {
		return &Error{Code: CodeInvalidItemState, Path: "pump.buffer.event", Phase: DeliveryPrecommit, Err: errInvalidTransition}
	}
	nextBytes := b.bytes + len(encoded)
	oversized := false
	if nextBytes > b.options.MaxPrecommitBytes {
		oversized = semantic && !b.oversizedUsed && isCompletedImageEvent(event)
		if !oversized {
			return b.limitError("pump.buffer.bytes")
		}
	}
	b.events = append(b.events, bufferedEvent{event: event, bytes: len(encoded)})
	b.bytes = nextBytes
	b.oversizedUsed = b.oversizedUsed || oversized
	if len(b.events) > b.peakEvents {
		b.peakEvents = len(b.events)
	}
	if b.bytes > b.peakBytes {
		b.peakBytes = b.bytes
	}
	return nil
}

func (b *precommitBuffer) checkpoint() bufferCheckpoint {
	if b == nil {
		return bufferCheckpoint{}
	}
	return bufferCheckpoint{count: len(b.events), bytes: b.bytes, peakEvents: b.peakEvents, peakBytes: b.peakBytes, oversizedUsed: b.oversizedUsed}
}

func (b *precommitBuffer) restore(checkpoint bufferCheckpoint) {
	if b == nil || checkpoint.count < 0 || checkpoint.count > len(b.events) {
		return
	}
	for index := checkpoint.count; index < len(b.events); index++ {
		b.events[index] = bufferedEvent{}
	}
	b.events = b.events[:checkpoint.count]
	b.bytes = checkpoint.bytes
	b.peakEvents = checkpoint.peakEvents
	b.peakBytes = checkpoint.peakBytes
	b.oversizedUsed = checkpoint.oversizedUsed
}

func (b *precommitBuffer) Drain() []canonical.Event {
	if b == nil || len(b.events) == 0 {
		return nil
	}
	result := make([]canonical.Event, len(b.events))
	for index, entry := range b.events {
		result[index] = entry.event
	}
	b.events = nil
	b.bytes = 0
	b.oversizedUsed = false
	return result
}

func (b *precommitBuffer) Count() int {
	if b == nil {
		return 0
	}
	return len(b.events)
}
func (b *precommitBuffer) Bytes() int {
	if b == nil {
		return 0
	}
	return b.bytes
}
func (b *precommitBuffer) PeakEvents() int {
	if b == nil {
		return 0
	}
	return b.peakEvents
}
func (b *precommitBuffer) PeakBytes() int {
	if b == nil {
		return 0
	}
	return b.peakBytes
}

func (b *precommitBuffer) limitError(path string) error {
	return &Error{Code: CodePrecommitLimitExceeded, Path: path, Phase: DeliveryPrecommit, Err: errInvalidTransition}
}

func isCompletedImageEvent(event canonical.Event) bool {
	payload, ok := event.ContentCompleted()
	return ok && payload.Content().Type() == canonical.ContentType("image")
}
