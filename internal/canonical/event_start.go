package canonical

import (
	"strings"
	"unicode/utf8"
)

type ResponseStartedPayload struct{ model string }

func (p ResponseStartedPayload) Model() string { return p.model }
func (p ResponseStartedPayload) Valid() bool {
	return p.model != "" && strings.TrimSpace(p.model) == p.model && utf8.ValidString(p.model) && len(p.model) <= MaxModelNameBytes
}

func NewResponseStartedEvent(id EventID, responseID ResponseID, sequence uint64, model string) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("response_started"))
	if err != nil {
		return Event{}, err
	}
	payload := ResponseStartedPayload{model: model}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.response_started")
	}
	event.responseStarted = payload
	return event, nil
}

// ItemStartParams contains metadata needed to begin one output item.
type ItemStartParams struct {
	Index     int
	ID        ItemID
	Type      ItemType
	Role      Role
	CallID    ToolCallID
	Namespace string
	Name      string
}

type ItemStartedPayload struct {
	index     int
	id        ItemID
	kind      ItemType
	role      Role
	callID    ToolCallID
	namespace string
	name      string
}

func (p ItemStartedPayload) Index() int         { return p.index }
func (p ItemStartedPayload) ID() ItemID         { return p.id }
func (p ItemStartedPayload) Type() ItemType     { return p.kind }
func (p ItemStartedPayload) Role() Role         { return p.role }
func (p ItemStartedPayload) CallID() ToolCallID { return p.callID }
func (p ItemStartedPayload) Namespace() string  { return p.namespace }
func (p ItemStartedPayload) Name() string       { return p.name }
func (p ItemStartedPayload) Valid() bool {
	if p.index < 0 || p.index >= MaxResponseItems || !p.id.Valid() {
		return false
	}
	switch p.kind {
	case ItemType("message"):
		return p.role == Role("assistant") && !p.callID.Valid() && p.namespace == "" && p.name == ""
	case ItemType("tool_call"):
		return p.role == "" && p.callID.Valid() && (p.namespace == "" || validToolName(p.namespace)) && validToolName(p.name)
	case ItemType("reasoning_summary"):
		return p.role == "" && !p.callID.Valid() && p.namespace == "" && p.name == ""
	default:
		return false
	}
}

func NewItemStartedEvent(id EventID, responseID ResponseID, sequence uint64, params ItemStartParams) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("item_started"))
	if err != nil {
		return Event{}, err
	}
	payload := ItemStartedPayload{index: params.Index, id: params.ID, kind: params.Type, role: params.Role, callID: params.CallID, namespace: params.Namespace, name: params.Name}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.item_started")
	}
	event.itemStarted = payload
	return event, nil
}

type ContentStartedPayload struct {
	itemID ItemID
	index  int
	kind   ContentType
}

func (p ContentStartedPayload) ItemID() ItemID    { return p.itemID }
func (p ContentStartedPayload) Index() int        { return p.index }
func (p ContentStartedPayload) Type() ContentType { return p.kind }
func (p ContentStartedPayload) Valid() bool {
	return p.itemID.Valid() && p.index >= 0 && p.index < MaxContentBlocks && p.kind.Valid()
}

func NewContentStartedEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, index int, kind ContentType) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("content_started"))
	if err != nil {
		return Event{}, err
	}
	payload := ContentStartedPayload{itemID: itemID, index: index, kind: kind}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.content_started")
	}
	event.contentStarted = payload
	return event, nil
}
