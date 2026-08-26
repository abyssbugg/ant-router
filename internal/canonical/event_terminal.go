package canonical

type ResponseCompletedPayload struct {
	finish  FinishReason
	usage   Usage
	itemIDs []ItemID
}

func (p ResponseCompletedPayload) FinishReason() FinishReason { return p.finish }
func (p ResponseCompletedPayload) Usage() Usage               { return p.usage }
func (p ResponseCompletedPayload) ItemIDs() []ItemID          { return append([]ItemID(nil), p.itemIDs...) }
func (p ResponseCompletedPayload) Valid() bool {
	if !p.finish.Valid() || p.finish == FinishReason("cancelled") || p.finish == FinishReason("error") || !p.usage.Valid() || len(p.itemIDs) > MaxResponseItems {
		return false
	}
	seen := make(map[ItemID]struct{}, len(p.itemIDs))
	for _, id := range p.itemIDs {
		if !id.Valid() {
			return false
		}
		if _, ok := seen[id]; ok {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func NewResponseCompletedEvent(id EventID, responseID ResponseID, sequence uint64, finish FinishReason, usage Usage, itemIDs []ItemID) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("response_completed"))
	if err != nil {
		return Event{}, err
	}
	payload := ResponseCompletedPayload{finish: finish, usage: usage, itemIDs: append([]ItemID(nil), itemIDs...)}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.response_completed")
	}
	event.completed = payload
	return event, nil
}

type ResponseFailedPayload struct {
	failure  Failure
	usage    Usage
	hasUsage bool
}

func (p ResponseFailedPayload) Failure() Failure     { return p.failure }
func (p ResponseFailedPayload) Usage() (Usage, bool) { return p.usage, p.hasUsage }
func (p ResponseFailedPayload) Valid() bool {
	return p.failure.Valid() && p.failure.Code() != FailureCode("cancelled") && (!p.hasUsage || p.usage.Valid())
}

func NewResponseFailedEvent(id EventID, responseID ResponseID, sequence uint64, failure Failure, usage *Usage) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("response_failed"))
	if err != nil {
		return Event{}, err
	}
	payload := ResponseFailedPayload{failure: failure}
	if usage != nil {
		payload.usage, payload.hasUsage = *usage, true
	}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.response_failed")
	}
	event.failed = payload
	return event, nil
}

type ResponseCancelledPayload struct {
	failure  Failure
	usage    Usage
	hasUsage bool
}

func (p ResponseCancelledPayload) Failure() Failure     { return p.failure }
func (p ResponseCancelledPayload) Usage() (Usage, bool) { return p.usage, p.hasUsage }
func (p ResponseCancelledPayload) Valid() bool {
	return p.failure.Valid() && p.failure.Code() == FailureCode("cancelled") && (!p.hasUsage || p.usage.Valid())
}

func NewResponseCancelledEvent(id EventID, responseID ResponseID, sequence uint64, failure Failure, usage *Usage) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("response_cancelled"))
	if err != nil {
		return Event{}, err
	}
	payload := ResponseCancelledPayload{failure: failure}
	if usage != nil {
		payload.usage, payload.hasUsage = *usage, true
	}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.response_cancelled")
	}
	event.cancelled = payload
	return event, nil
}
