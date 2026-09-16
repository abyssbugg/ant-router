package canonical

import "unicode/utf8"

// deltaEventEnvelopeBytes bounds the fixed JSON overhead around a delta
// payload in its encoded event (version, IDs, sequence, type, field names).
// The worst-case delta envelope is 218 bytes; 256 adds safety margin.
const deltaEventEnvelopeBytes = 256

// escapedJSONSize returns the encoded length of s as a JSON string value,
// accounting for escape inflation (quotes and backslashes double; control
// characters and HTML-significant bytes expand to six-byte unicode escapes).
func escapedJSONSize(s string) int {
	size := len(s)
	for index := 0; index < len(s); {
		byte_ := s[index]
		switch {
		case byte_ == '"' || byte_ == '\\' || byte_ == '\n' || byte_ == '\r' || byte_ == '\t' || byte_ == '\b' || byte_ == '\f':
			size++
			index++
		case byte_ < 0x20 || byte_ == '<' || byte_ == '>' || byte_ == '&':
			size += 5
			index++
		case byte_ == 0xE2 && index+2 < len(s) && s[index+1] == 0x80 && (s[index+2] == 0xA8 || s[index+2] == 0xA9):
			size += 3
			index += 3
		default:
			index++
		}
	}
	return size
}

// deltaExceedsWireLimit reports whether a delta's encoded form cannot fit an
// ordinary event once escape inflation and the event envelope are counted.
func deltaExceedsWireLimit(delta string) bool {
	return len(delta) > MaxOrdinaryEventBytes || escapedJSONSize(delta)+deltaEventEnvelopeBytes > MaxOrdinaryEventBytes
}

type DeltaPayload struct {
	itemID       ItemID
	contentIndex int
	delta        string
}

func (p DeltaPayload) ItemID() ItemID    { return p.itemID }
func (p DeltaPayload) ContentIndex() int { return p.contentIndex }
func (p DeltaPayload) Delta() string     { return p.delta }
func (p DeltaPayload) Valid() bool {
	return p.itemID.Valid() && p.contentIndex >= 0 && p.contentIndex < MaxContentBlocks && p.delta != "" && utf8.ValidString(p.delta) && !deltaExceedsWireLimit(p.delta)
}

type ToolArgumentsDeltaPayload struct {
	itemID ItemID
	delta  string
}

func (p ToolArgumentsDeltaPayload) ItemID() ItemID { return p.itemID }
func (p ToolArgumentsDeltaPayload) Delta() string  { return p.delta }
func (p ToolArgumentsDeltaPayload) Valid() bool {
	return p.itemID.Valid() && p.delta != "" && utf8.ValidString(p.delta) && !deltaExceedsWireLimit(p.delta)
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
		} else if deltaExceedsWireLimit(delta) {
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
		} else if deltaExceedsWireLimit(delta) {
			code = CodeLimitExceeded
		}
		return Event{}, eventError(code, "event.tool_arguments_delta")
	}
	event.argumentsDelta = payload
	return event, nil
}
