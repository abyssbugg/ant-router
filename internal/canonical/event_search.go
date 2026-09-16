package canonical

// HostedSearchStatusPayload contains one hosted-search state transition.
type HostedSearchStatusPayload struct {
	itemID ItemID
	status HostedSearchStatus
}

func (p HostedSearchStatusPayload) ItemID() ItemID             { return p.itemID }
func (p HostedSearchStatusPayload) Status() HostedSearchStatus { return p.status }
func (p HostedSearchStatusPayload) Valid() bool                { return p.itemID.Valid() && p.status.Valid() }

// NewHostedSearchStatusEvent validates a hosted-search state transition.
func NewHostedSearchStatusEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, status HostedSearchStatus) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("hosted_search_status"))
	if err != nil {
		return Event{}, err
	}
	payload := HostedSearchStatusPayload{itemID: itemID, status: status}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.hosted_search_status")
	}
	event.hostedSearchStatus = payload
	return event, nil
}

// URLCitationAddedPayload contains one URL citation annotation for text content.
type URLCitationAddedPayload struct {
	itemID       ItemID
	contentIndex int
	citation     URLCitation
}

func (p URLCitationAddedPayload) ItemID() ItemID        { return p.itemID }
func (p URLCitationAddedPayload) ContentIndex() int     { return p.contentIndex }
func (p URLCitationAddedPayload) Citation() URLCitation { return p.citation }
func (p URLCitationAddedPayload) Valid() bool {
	return p.itemID.Valid() && p.contentIndex >= 0 && p.contentIndex < MaxContentBlocks && p.citation.Valid()
}

// NewURLCitationAddedEvent validates a URL citation annotation event.
func NewURLCitationAddedEvent(id EventID, responseID ResponseID, sequence uint64, itemID ItemID, contentIndex int, citation URLCitation) (Event, error) {
	event, err := newEvent(id, responseID, sequence, EventType("url_citation_added"))
	if err != nil {
		return Event{}, err
	}
	payload := URLCitationAddedPayload{itemID: itemID, contentIndex: contentIndex, citation: citation}
	if !payload.Valid() {
		return Event{}, eventError(CodeInvalidInvariant, "event.url_citation_added")
	}
	event.urlCitationAdded = payload
	return event, nil
}
