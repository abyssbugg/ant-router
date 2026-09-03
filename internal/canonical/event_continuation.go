package canonical

// ContinuationUpdatedPayload carries a complete cumulative opaque continuation.
type ContinuationUpdatedPayload struct{ continuation Continuation }

// Continuation returns the immutable continuation value.
func (p ContinuationUpdatedPayload) Continuation() Continuation { return p.continuation }

// Valid reports whether the payload contains a valid continuation.
func (p ContinuationUpdatedPayload) Valid() bool { return p.continuation.Valid() }

// NewContinuationUpdatedEvent constructs a continuation update event.
func NewContinuationUpdatedEvent(id EventID, responseID ResponseID, sequence uint64, continuation Continuation) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("continuation_updated"))
	if err != nil {
		return Event{}, err
	}
	payload := ContinuationUpdatedPayload{continuation: continuation}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.continuation_updated")
	}
	event.continuationUpdated = payload
	return event, nil
}
