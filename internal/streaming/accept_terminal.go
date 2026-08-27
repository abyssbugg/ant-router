package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

func (m *Machine) acceptTerminal(event canonical.Event) (acceptOutcome, error) {
	if payload, ok := event.ResponseCompleted(); ok {
		return m.acceptResponseCompleted(payload)
	}
	if payload, ok := event.ResponseFailed(); ok {
		failure := payload.Failure()
		var usage *canonical.Usage
		if value, present := payload.Usage(); present {
			if !value.AtLeast(m.latestUsage) {
				return acceptOutcome{}, m.error(CodeUsageRegression, "event.response_failed.usage")
			}
			usage = &value
		}
		return acceptOutcome{terminal: TerminalFailed, failure: &failure, usage: usage}, nil
	}
	payload, ok := event.ResponseCancelled()
	if !ok {
		return acceptOutcome{}, m.error(CodeTerminalMismatch, "event.terminal")
	}
	failure := payload.Failure()
	var usage *canonical.Usage
	if value, present := payload.Usage(); present {
		if !value.AtLeast(m.latestUsage) {
			return acceptOutcome{}, m.error(CodeUsageRegression, "event.response_cancelled.usage")
		}
		usage = &value
	}
	return acceptOutcome{terminal: TerminalCancelled, failure: &failure, usage: usage}, nil
}

func (m *Machine) acceptResponseCompleted(payload canonical.ResponseCompletedPayload) (acceptOutcome, error) {
	ids := payload.ItemIDs()
	if len(ids) != len(m.itemOrder) {
		return acceptOutcome{}, m.error(CodeTerminalMismatch, "event.response_completed.item_ids")
	}
	items := make([]canonical.Item, len(m.itemOrder))
	for index, id := range m.itemOrder {
		state := m.items[id]
		if state == nil || !state.completed || !state.finalItem.Valid() || ids[index] != id {
			return acceptOutcome{}, m.error(CodeTerminalMismatch, "event.response_completed.item_ids")
		}
		items[index] = state.finalItem
	}
	if !payload.Usage().AtLeast(m.latestUsage) {
		return acceptOutcome{}, m.error(CodeUsageRegression, "event.response_completed.usage")
	}
	response, err := canonical.NewResponse(canonical.ResponseParams{ID: m.responseID, Model: m.model, Items: items, FinishReason: payload.FinishReason(), Usage: payload.Usage()})
	if err != nil {
		return acceptOutcome{}, m.error(CodeTerminalMismatch, "event.response_completed")
	}
	usage := payload.Usage()
	return acceptOutcome{semantic: !m.committed, terminal: TerminalCompleted, response: &response, usage: &usage}, nil
}
