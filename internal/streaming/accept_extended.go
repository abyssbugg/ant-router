package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

func (m *Machine) acceptCustomInputDelta(event canonical.Event) (acceptOutcome, error) {
	payload, ok := event.CustomInputDelta()
	if !ok {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.custom_input_delta")
	}
	item := m.items[payload.ItemID()]
	if item == nil || item.completed || item.kind != canonical.ItemType("custom_tool_call") || item.customInputCompleted {
		return acceptOutcome{}, m.error(CodeInvalidItemState, "event.custom_input_delta")
	}
	copyItem := item.clone()
	nextBytes := m.reconstructedBytes + len(payload.Delta())
	if nextBytes > m.options.MaxResponseBytes || len(copyItem.customInput)+len(payload.Delta()) > canonical.MaxCustomToolInputBytes {
		return acceptOutcome{}, m.error(CodeResponseLimitExceeded, "event.custom_input_delta")
	}
	copyItem.customInput = append(copyItem.customInput, payload.Delta()...)
	m.items[payload.ItemID()] = copyItem
	m.reconstructedBytes = nextBytes
	return acceptOutcome{semantic: true}, nil
}

func (m *Machine) acceptCustomInputCompleted(event canonical.Event) (acceptOutcome, error) {
	payload, ok := event.CustomInputCompleted()
	if !ok {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.custom_input_completed")
	}
	item := m.items[payload.ItemID()]
	if item == nil || item.completed || item.kind != canonical.ItemType("custom_tool_call") || item.customInputCompleted {
		return acceptOutcome{}, m.error(CodeInvalidItemState, "event.custom_input_completed")
	}
	copyItem := item.clone()
	atomicBytes := 0
	if len(item.customInput) > 0 {
		if string(item.customInput) != payload.Input() {
			return acceptOutcome{}, m.error(CodeCompletionMismatch, "event.custom_input_completed")
		}
	} else {
		atomicBytes = len(payload.Input())
		if m.reconstructedBytes+atomicBytes > m.options.MaxResponseBytes {
			return acceptOutcome{}, m.error(CodeResponseLimitExceeded, "event.custom_input_completed")
		}
		copyItem.customInput = append([]byte(nil), payload.Input()...)
	}
	copyItem.customInputCompleted = true
	m.items[payload.ItemID()] = copyItem
	m.reconstructedBytes += atomicBytes
	return acceptOutcome{semantic: len(item.customInput) == 0}, nil
}

func (m *Machine) acceptHostedSearchStatus(event canonical.Event) (acceptOutcome, error) {
	payload, ok := event.HostedSearchStatus()
	if !ok {
		return acceptOutcome{}, m.error(CodeInvalidItemState, "event.hosted_search_status")
	}
	item := m.items[payload.ItemID()]
	if item == nil || item.completed || item.kind != canonical.ItemType("hosted_search_call") || !validHostedSearchTransition(item.searchStatus, payload.Status()) {
		return acceptOutcome{}, m.error(CodeInvalidItemState, "event.hosted_search_status")
	}
	copyItem := item.clone()
	copyItem.searchStatus = payload.Status()
	m.items[payload.ItemID()] = copyItem
	return acceptOutcome{semantic: true}, nil
}

func validHostedSearchTransition(current, next canonical.HostedSearchStatus) bool {
	switch current {
	case canonical.HostedSearchStatusInProgress:
		return next == canonical.HostedSearchStatusSearching || next.Terminal()
	case canonical.HostedSearchStatusSearching:
		return next.Terminal()
	default:
		return false
	}
}

func (m *Machine) acceptURLCitationAdded(event canonical.Event) (acceptOutcome, error) {
	payload, ok := event.URLCitationAdded()
	if !ok {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.url_citation_added")
	}
	item := m.items[payload.ItemID()]
	if item == nil || item.completed {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.url_citation_added")
	}
	content := item.contents[payload.ContentIndex()]
	if content == nil || content.completed || content.kind != canonical.ContentType("text") || len(content.citations) >= canonical.MaxTextCitations {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.url_citation_added")
	}
	citation := payload.Citation()
	if len(content.citations) > 0 {
		previous := content.citations[len(content.citations)-1]
		if citation.StartIndex() < previous.StartIndex() || citationsEqual(previous, citation) {
			return acceptOutcome{}, m.error(CodeInvalidContentState, "event.url_citation_added")
		}
	}
	addedBytes := citationSize(citation)
	if m.reconstructedBytes+addedBytes > m.options.MaxResponseBytes {
		return acceptOutcome{}, m.error(CodeResponseLimitExceeded, "event.url_citation_added")
	}
	copyItem := item.clone()
	copyContent := copyItem.contents[payload.ContentIndex()]
	copyContent.citations = append(copyContent.citations, citation)
	m.items[payload.ItemID()] = copyItem
	m.reconstructedBytes += addedBytes
	return acceptOutcome{semantic: true}, nil
}
