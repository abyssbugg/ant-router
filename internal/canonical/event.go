package canonical

import "errors"

// Event is an immutable stateless canonical stream event.
type Event struct {
	id                   EventID
	responseID           ResponseID
	sequence             uint64
	kind                 EventType
	responseStarted      ResponseStartedPayload
	continuationUpdated  ContinuationUpdatedPayload
	itemStarted          ItemStartedPayload
	contentStarted       ContentStartedPayload
	textDelta            DeltaPayload
	jsonDelta            DeltaPayload
	argumentsDelta       ToolArgumentsDeltaPayload
	customInputDelta     CustomInputDeltaPayload
	customInputCompleted CustomInputCompletedPayload
	hostedSearchStatus   HostedSearchStatusPayload
	urlCitationAdded     URLCitationAddedPayload
	contentCompleted     ContentCompletedPayload
	itemCompleted        ItemCompletedPayload
	usageUpdated         UsageUpdatedPayload
	completed            ResponseCompletedPayload
	failed               ResponseFailedPayload
	cancelled            ResponseCancelledPayload
}

func (e Event) ID() EventID            { return e.id }
func (e Event) ResponseID() ResponseID { return e.responseID }
func (e Event) Sequence() uint64       { return e.sequence }
func (e Event) Type() EventType        { return e.kind }
func (e Event) Valid() bool {
	if !e.id.Valid() || !e.responseID.Valid() || !e.kind.Valid() {
		return false
	}
	switch e.kind {
	case EventType("response_started"):
		return e.responseStarted.Valid()
	case EventType("continuation_updated"):
		return e.continuationUpdated.Valid()
	case EventType("item_started"):
		return e.itemStarted.Valid()
	case EventType("content_started"):
		return e.contentStarted.Valid()
	case EventType("text_delta"):
		return e.textDelta.Valid()
	case EventType("json_delta"):
		return e.jsonDelta.Valid()
	case EventType("tool_arguments_delta"):
		return e.argumentsDelta.Valid()
	case EventType("custom_input_delta"):
		return e.customInputDelta.Valid()
	case EventType("custom_input_completed"):
		return e.customInputCompleted.Valid()
	case EventType("hosted_search_status"):
		return e.hostedSearchStatus.Valid()
	case EventType("url_citation_added"):
		return e.urlCitationAdded.Valid()
	case EventType("content_completed"):
		return e.contentCompleted.Valid()
	case EventType("item_completed"):
		return e.itemCompleted.Valid()
	case EventType("usage_updated"):
		return e.usageUpdated.Valid()
	case EventType("response_completed"):
		return e.completed.Valid()
	case EventType("response_failed"):
		return e.failed.Valid()
	case EventType("response_cancelled"):
		return e.cancelled.Valid()
	default:
		return false
	}
}

func (e Event) ResponseStarted() (ResponseStartedPayload, bool) {
	return e.responseStarted, e.kind == EventType("response_started")
}
func (e Event) ContinuationUpdated() (ContinuationUpdatedPayload, bool) {
	return e.continuationUpdated, e.kind == EventType("continuation_updated")
}
func (e Event) ItemStarted() (ItemStartedPayload, bool) {
	return e.itemStarted, e.kind == EventType("item_started")
}
func (e Event) ContentStarted() (ContentStartedPayload, bool) {
	return e.contentStarted, e.kind == EventType("content_started")
}
func (e Event) TextDelta() (DeltaPayload, bool) {
	return e.textDelta, e.kind == EventType("text_delta")
}
func (e Event) JSONDelta() (DeltaPayload, bool) {
	return e.jsonDelta, e.kind == EventType("json_delta")
}
func (e Event) ToolArgumentsDelta() (ToolArgumentsDeltaPayload, bool) {
	return e.argumentsDelta, e.kind == EventType("tool_arguments_delta")
}
func (e Event) CustomInputDelta() (CustomInputDeltaPayload, bool) {
	return e.customInputDelta, e.kind == EventType("custom_input_delta")
}
func (e Event) CustomInputCompleted() (CustomInputCompletedPayload, bool) {
	return e.customInputCompleted, e.kind == EventType("custom_input_completed")
}
func (e Event) HostedSearchStatus() (HostedSearchStatusPayload, bool) {
	return e.hostedSearchStatus, e.kind == EventType("hosted_search_status")
}
func (e Event) URLCitationAdded() (URLCitationAddedPayload, bool) {
	return e.urlCitationAdded, e.kind == EventType("url_citation_added")
}
func (e Event) ContentCompleted() (ContentCompletedPayload, bool) {
	return e.contentCompleted, e.kind == EventType("content_completed")
}
func (e Event) ItemCompleted() (ItemCompletedPayload, bool) {
	return e.itemCompleted, e.kind == EventType("item_completed")
}
func (e Event) UsageUpdated() (UsageUpdatedPayload, bool) {
	return e.usageUpdated, e.kind == EventType("usage_updated")
}
func (e Event) ResponseCompleted() (ResponseCompletedPayload, bool) {
	return e.completed, e.kind == EventType("response_completed")
}
func (e Event) ResponseFailed() (ResponseFailedPayload, bool) {
	return e.failed, e.kind == EventType("response_failed")
}
func (e Event) ResponseCancelled() (ResponseCancelledPayload, bool) {
	return e.cancelled, e.kind == EventType("response_cancelled")
}

func newEvent(id EventID, responseID ResponseID, sequence uint64, kind EventType) (Event, error) {
	if !id.Valid() || !responseID.Valid() {
		return Event{}, eventError(CodeInvalidID, "event")
	}
	if !kind.Valid() {
		return Event{}, eventError(CodeInvalidVariant, "event.type")
	}
	return Event{id: id, responseID: responseID, sequence: sequence, kind: kind}, nil
}

func eventError(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Err: errors.New("event is invalid")}
}
