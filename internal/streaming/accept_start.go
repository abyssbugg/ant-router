package streaming

import "github.com/abyssbugg/ant-router/internal/canonical"

func (m *Machine) acceptItemStarted(event canonical.Event) error {
	payload, ok := event.ItemStarted()
	if !ok || !payload.Valid() {
		return m.error(CodeInvalidItemState, "event.item_started")
	}
	if payload.Index() != len(m.itemOrder) {
		return m.error(CodeInvalidItemState, "event.item_started.index")
	}
	if len(m.itemOrder) >= m.options.MaxActiveItems {
		return m.error(CodeResponseLimitExceeded, "event.item_started")
	}
	if _, exists := m.items[payload.ID()]; exists {
		return m.error(CodeInvalidItemState, "event.item_started.id")
	}
	nextBytes := m.reconstructedBytes
	if payload.Type() == canonical.ItemType("hosted_search_call") {
		nextBytes += hostedSearchActionSize(payload.SearchAction())
		if nextBytes > m.options.MaxResponseBytes {
			return m.error(CodeResponseLimitExceeded, "event.item_started")
		}
	}
	m.items[payload.ID()] = newItemState(payload)
	m.itemOrder = append(m.itemOrder, payload.ID())
	m.reconstructedBytes = nextBytes
	return nil
}

func hostedSearchActionSize(action canonical.HostedSearchAction) int {
	size := len(action.URL()) + len(action.Pattern())
	for _, query := range action.Queries() {
		size += len(query)
	}
	for _, source := range action.Sources() {
		size += len(source)
	}
	return size
}

func (m *Machine) acceptContentStarted(event canonical.Event) error {
	payload, ok := event.ContentStarted()
	if !ok || !payload.Valid() {
		return m.error(CodeInvalidContentState, "event.content_started")
	}
	item, exists := m.items[payload.ItemID()]
	if !exists || item.completed || item.kind == canonical.ItemType("tool_call") || item.kind == canonical.ItemType("custom_tool_call") || item.kind == canonical.ItemType("hosted_search_call") {
		return m.error(CodeInvalidContentState, "event.content_started.item_id")
	}
	if payload.Index() < 0 || payload.Index() >= m.options.MaxOpenContentPerItem {
		return m.error(CodeResponseLimitExceeded, "event.content_started.index")
	}
	if payload.Index() != item.nextContent {
		return m.error(CodeInvalidContentState, "event.content_started.index")
	}
	if len(item.contents) >= m.options.MaxOpenContentPerItem {
		return m.error(CodeResponseLimitExceeded, "event.content_started")
	}
	if item.kind == canonical.ItemType("reasoning_summary") && payload.Type() != canonical.ContentType("text") {
		return m.error(CodeInvalidContentState, "event.content_started.type")
	}
	item.contents[payload.Index()] = &contentState{index: payload.Index(), kind: payload.Type()}
	item.nextContent++
	return nil
}
