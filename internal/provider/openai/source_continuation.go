package openai

import (
	"github.com/abyssbugg/ant-router/internal/canonical"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
)

const (
	maxBufferedReasoningEvents = 4096
	maxBufferedReasoningBytes  = canonical.MaxRequestBytes
)

type bufferedReasoningEventKind uint8

const (
	bufferedReasoningStart bufferedReasoningEventKind = iota + 1
	bufferedReasoningDelta
	bufferedReasoningCompleted
)

type bufferedReasoningEvent struct {
	kind  bufferedReasoningEventKind
	index int
	text  string
	block canonical.ContentBlock
}

func (item *sourceItem) bufferReasoning(event bufferedReasoningEvent, size int) error {
	if item == nil || size < 0 || len(item.reasoningEvents) >= maxBufferedReasoningEvents || item.reasoningBytes > maxBufferedReasoningBytes-size {
		return streamError("stream.reasoning.buffer")
	}
	item.reasoningEvents = append(item.reasoningEvents, event)
	item.reasoningBytes += size
	return nil
}

func (s *eventSource) hasUnfinishedReasoning() bool {
	for _, state := range s.items {
		if state.kind == canonical.ItemType("reasoning_summary") && !state.reasoningDone {
			return true
		}
	}
	return false
}

func (s *eventSource) startToolCallWithContinuation(state *sourceItem, upstreamItemID string, params canonical.ItemStartParams) (canonical.Event, bool, error) {
	entry, err := openaicontinuation.NewFunctionCallEntry(state.id, state.callID, upstreamItemID, state.upstreamCallID)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.tool.continuation")
	}
	entries := append(append([]openaicontinuation.ContinuationEntry(nil), s.continuationEntries...), entry)
	continuation, err := openaicontinuation.EncodeContinuation(s.providerID, s.upstreamModel, entries)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.tool.continuation")
	}
	continuationEvent, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContinuationUpdatedEvent(id, s.responseID, sequence, continuation)
	})
	if err != nil {
		return canonical.Event{}, false, s.wrapCanonicalError(err)
	}
	itemEvent, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemStartedEvent(id, s.responseID, sequence, params)
	})
	if err != nil {
		return canonical.Event{}, false, s.wrapCanonicalError(err)
	}
	s.items[upstreamItemID] = state
	s.orderedItemIDs = append(s.orderedItemIDs, state.id)
	s.continuationEntries = entries
	s.pending = append(s.pending, itemEvent)
	return continuationEvent, true, nil
}

func (s *eventSource) completeReasoning(state *sourceItem, upstreamID, encryptedContent string, item canonical.Item) (canonical.Event, bool, error) {
	entry, err := openaicontinuation.NewContinuationEntry(state.id, upstreamID, encryptedContent)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.reasoning.continuation")
	}
	entries := append(append([]openaicontinuation.ContinuationEntry(nil), s.continuationEntries...), entry)
	continuation, err := openaicontinuation.EncodeContinuation(s.providerID, s.upstreamModel, entries)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.reasoning.continuation")
	}
	events := make([]canonical.Event, 0, len(state.reasoningEvents)+3)
	appendEvent := func(build func(canonical.EventID, uint64) (canonical.Event, error)) error {
		event, buildErr := s.newEvent(build)
		if buildErr != nil {
			return s.wrapCanonicalError(buildErr)
		}
		events = append(events, event)
		return nil
	}
	if err := appendEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContinuationUpdatedEvent(id, s.responseID, sequence, continuation)
	}); err != nil {
		return canonical.Event{}, false, err
	}
	if err := appendEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemStartedEvent(id, s.responseID, sequence, canonical.ItemStartParams{Index: state.index, ID: state.id, Type: canonical.ItemType("reasoning_summary")})
	}); err != nil {
		return canonical.Event{}, false, err
	}
	for _, pending := range state.reasoningEvents {
		var build func(canonical.EventID, uint64) (canonical.Event, error)
		switch pending.kind {
		case bufferedReasoningStart:
			build = func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
				return canonical.NewContentStartedEvent(id, s.responseID, sequence, state.id, pending.index, canonical.ContentType("text"))
			}
		case bufferedReasoningDelta:
			build = func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
				return canonical.NewTextDeltaEvent(id, s.responseID, sequence, state.id, pending.index, pending.text)
			}
		case bufferedReasoningCompleted:
			build = func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
				return canonical.NewContentCompletedEvent(id, s.responseID, sequence, state.id, pending.index, pending.block)
			}
		default:
			return canonical.Event{}, false, streamError("stream.reasoning.buffer")
		}
		if err := appendEvent(build); err != nil {
			return canonical.Event{}, false, err
		}
	}
	if err := appendEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemCompletedEvent(id, s.responseID, sequence, state.index, item)
	}); err != nil {
		return canonical.Event{}, false, err
	}
	if len(events) == 0 {
		return canonical.Event{}, false, streamError("stream.reasoning.buffer")
	}
	state.reasoningDone = true
	s.continuationEntries = entries
	s.pending = append(s.pending, events[1:]...)
	return events[0], true, nil
}
