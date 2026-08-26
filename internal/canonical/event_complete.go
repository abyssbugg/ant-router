package canonical

type ContentCompletedPayload struct {
	itemID  ItemID
	index   int
	content ContentBlock
}

func (p ContentCompletedPayload) ItemID() ItemID        { return p.itemID }
func (p ContentCompletedPayload) Index() int            { return p.index }
func (p ContentCompletedPayload) Content() ContentBlock { return p.content }
func (p ContentCompletedPayload) Valid() bool {
	return p.itemID.Valid() && p.index >= 0 && p.index < MaxContentBlocks && p.content.Valid()
}

func NewContentCompletedEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, index int, content ContentBlock) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("content_completed"))
	if err != nil {
		return Event{}, err
	}
	payload := ContentCompletedPayload{itemID: itemID, index: index, content: content}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.content_completed")
	}
	event.contentCompleted = payload
	return event, nil
}

type ItemCompletedPayload struct {
	index int
	item  Item
}

func (p ItemCompletedPayload) Index() int { return p.index }
func (p ItemCompletedPayload) Item() Item { return p.item }
func (p ItemCompletedPayload) Valid() bool {
	return p.index >= 0 && p.index < MaxResponseItems && p.item.Valid() && p.item.Type() != ItemType("tool_result")
}

func NewItemCompletedEvent(id EventID, responseID ResponseID, sequence uint64, index int, item Item) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("item_completed"))
	if err != nil {
		return Event{}, err
	}
	payload := ItemCompletedPayload{index: index, item: item}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.item_completed")
	}
	event.itemCompleted = payload
	return event, nil
}

type UsageUpdatedPayload struct{ usage Usage }

func (p UsageUpdatedPayload) Usage() Usage { return p.usage }
func (p UsageUpdatedPayload) Valid() bool  { return p.usage.Valid() }

func NewUsageUpdatedEvent(id EventID, responseID ResponseID, sequence uint64, usage Usage) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("usage_updated"))
	if err != nil {
		return Event{}, err
	}
	payload := UsageUpdatedPayload{usage: usage}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.usage")
	}
	event.usageUpdated = payload
	return event, nil
}
