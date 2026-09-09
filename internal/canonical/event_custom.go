package canonical

import "unicode/utf8"

// CustomInputDeltaPayload contains one freeform custom-tool input fragment.
type CustomInputDeltaPayload struct {
	itemID ItemID
	delta  string
}

func (p CustomInputDeltaPayload) ItemID() ItemID { return p.itemID }
func (p CustomInputDeltaPayload) Delta() string  { return p.delta }
func (p CustomInputDeltaPayload) Valid() bool {
	return p.itemID.Valid() && p.delta != "" && utf8.ValidString(p.delta) && len(p.delta) <= MaxOrdinaryEventBytes && validCustomTextCharacters(p.delta)
}

// NewCustomInputDeltaEvent validates one freeform custom-tool input fragment.
func NewCustomInputDeltaEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, delta string) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("custom_input_delta"))
	if err != nil {
		return Event{}, err
	}
	payload := CustomInputDeltaPayload{itemID: itemID, delta: delta}
	if !payload.Valid() {
		code := CodeInvalidInvariant
		if !utf8.ValidString(delta) {
			code = CodeInvalidUTF8
		} else if len(delta) > MaxOrdinaryEventBytes {
			code = CodeLimitExceeded
		}
		return Event{}, eventError(code, "event.custom_input_delta")
	}
	event.customInputDelta = payload
	return event, nil
}

// CustomInputCompletedPayload contains the authoritative freeform custom-tool input.
type CustomInputCompletedPayload struct {
	itemID ItemID
	input  string
}

func (p CustomInputCompletedPayload) ItemID() ItemID { return p.itemID }
func (p CustomInputCompletedPayload) Input() string  { return p.input }
func (p CustomInputCompletedPayload) Valid() bool {
	return p.itemID.Valid() && utf8.ValidString(p.input) && len(p.input) <= MaxCustomToolInputBytes && validCustomTextCharacters(p.input)
}

// NewCustomInputCompletedEvent validates authoritative freeform custom-tool input.
func NewCustomInputCompletedEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, input string) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("custom_input_completed"))
	if err != nil {
		return Event{}, err
	}
	payload := CustomInputCompletedPayload{itemID: itemID, input: input}
	if !payload.Valid() {
		code := CodeInvalidInvariant
		if !utf8.ValidString(input) {
			code = CodeInvalidUTF8
		} else if len(input) > MaxCustomToolInputBytes {
			code = CodeLimitExceeded
		}
		return Event{}, eventError(code, "event.custom_input_completed")
	}
	event.customInputCompleted = payload
	return event, nil
}
