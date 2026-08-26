package canonical

import "unicode/utf8"

type DeltaPayload struct {
	itemID       ItemID
	contentIndex int
	delta        string
}

func (p DeltaPayload) ItemID() ItemID    { return p.itemID }
func (p DeltaPayload) ContentIndex() int { return p.contentIndex }
func (p DeltaPayload) Delta() string     { return p.delta }
func (p DeltaPayload) Valid() bool {
	return p.itemID.Valid() && p.contentIndex >= 0 && p.contentIndex < MaxContentBlocks && p.delta != "" && utf8.ValidString(p.delta) && len(p.delta) <= MaxOrdinaryEventBytes
}

type ToolArgumentsDeltaPayload struct {
	itemID ItemID
	delta  string
}

func (p ToolArgumentsDeltaPayload) ItemID() ItemID { return p.itemID }
func (p ToolArgumentsDeltaPayload) Delta() string  { return p.delta }
func (p ToolArgumentsDeltaPayload) Valid() bool {
	return p.itemID.Valid() && p.delta != "" && utf8.ValidString(p.delta) && len(p.delta) <= MaxOrdinaryEventBytes
}

func NewTextDeltaEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, contentIndex int, delta string) (Event, error) {
	return newDeltaEvent(id, responseID, sequence, EventType("text_delta"), itemID, contentIndex, delta)
}
func NewJSONDeltaEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, contentIndex int, delta string) (Event, error) {
	return newDeltaEvent(id, responseID, sequence, EventType("json_delta"), itemID, contentIndex, delta)
}
func newDeltaEvent(id EventID, responseID ResponseID, sequence uint64, kind EventType, itemID ItemID, contentIndex int, delta string) (Event, error) {
	event, err := newEvent(id, responseID, sequence, kind)
	if err != nil {
		return Event{}, err
	}
	payload := DeltaPayload{itemID: itemID, contentIndex: contentIndex, delta: delta}
	if !payload.Valid() {
		code := CodeInvalidInvariant
		if !utf8.ValidString(delta) {
			code = CodeInvalidUTF8
		} else if len(delta) > MaxOrdinaryEventBytes {
			code = CodeLimitExceeded
		}
		return Event{}, eventError(code, "event.delta")
	}
	if kind == EventType("text_delta") {
		event.textDelta = payload
	} else {
		event.jsonDelta = payload
	}
	return event, nil
}

func NewToolArgumentsDeltaEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, delta string) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("tool_arguments_delta"))
	if err != nil {
		return Event{}, err
	}
	payload := ToolArgumentsDeltaPayload{itemID: itemID, delta: delta}
	if !payload.Valid() {
		code := CodeInvalidInvariant
		if !utf8.ValidString(delta) {
			code = CodeInvalidUTF8
		} else if len(delta) > MaxOrdinaryEventBytes {
			code = CodeLimitExceeded
		}
		return Event{}, eventError(code, "event.tool_arguments_delta")
	}
	event.argumentsDelta = payload
	return event, nil
}
