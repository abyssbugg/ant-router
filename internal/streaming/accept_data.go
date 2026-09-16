package streaming

import (
	"errors"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

var errInvalidTransition = errors.New("stream transition is invalid")

func (m *Machine) acceptDelta(event canonical.Event) (acceptOutcome, error) {
	if payload, ok := event.TextDelta(); ok {
		return m.acceptContentDelta(payload.ItemID(), payload.ContentIndex(), canonical.ContentType("text"), payload.Delta())
	}
	if payload, ok := event.JSONDelta(); ok {
		return m.acceptContentDelta(payload.ItemID(), payload.ContentIndex(), canonical.ContentType("json"), payload.Delta())
	}
	payload, ok := event.ToolArgumentsDelta()
	if !ok {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.delta")
	}
	item, exists := m.items[payload.ItemID()]
	if !exists || item.completed || item.kind != canonical.ItemType("tool_call") {
		return acceptOutcome{}, m.error(CodeInvalidItemState, "event.tool_arguments_delta")
	}
	copyItem := item.clone()
	nextBytes := m.reconstructedBytes + len(payload.Delta())
	if nextBytes > m.options.MaxResponseBytes || len(copyItem.arguments)+len(payload.Delta()) > canonical.MaxJSONBytes {
		return acceptOutcome{}, m.error(CodeResponseLimitExceeded, "event.tool_arguments_delta")
	}
	copyItem.arguments = append(copyItem.arguments, payload.Delta()...)
	m.items[payload.ItemID()] = copyItem
	m.reconstructedBytes = nextBytes
	return acceptOutcome{semantic: true}, nil
}

func (m *Machine) acceptContentDelta(itemID canonical.ItemID, index int, kind canonical.ContentType, delta string) (acceptOutcome, error) {
	item, exists := m.items[itemID]
	if !exists || item.completed {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.delta")
	}
	content, exists := item.contents[index]
	if !exists || content.completed || content.kind != kind || kind == canonical.ContentType("image") {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.delta")
	}
	if !utf8.ValidString(delta) {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.delta")
	}
	limit := canonical.MaxTextBytes
	if kind == canonical.ContentType("json") {
		limit = canonical.MaxJSONBytes
	}
	copyItem := item.clone()
	copyContent := copyItem.contents[index]
	if len(copyContent.buffer)+len(delta) > limit || m.reconstructedBytes+len(delta) > m.options.MaxResponseBytes {
		return acceptOutcome{}, m.error(CodeResponseLimitExceeded, "event.delta")
	}
	copyContent.buffer = append(copyContent.buffer, delta...)
	m.items[itemID] = copyItem
	m.reconstructedBytes += len(delta)
	return acceptOutcome{semantic: true}, nil
}

func (m *Machine) acceptContentCompleted(event canonical.Event) (acceptOutcome, error) {
	payload, ok := event.ContentCompleted()
	if !ok {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.content_completed")
	}
	item, exists := m.items[payload.ItemID()]
	if !exists || item.completed {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.content_completed")
	}
	content, exists := item.contents[payload.Index()]
	if !exists || content.completed || content.kind != payload.Content().Type() {
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.content_completed")
	}
	copyItem := item.clone()
	copyContent := copyItem.contents[payload.Index()]
	atomicSize := 0
	semantic := len(content.buffer) == 0
	switch content.kind {
	case canonical.ContentType("text"):
		final, _ := payload.Content().Text()
		finalCitations := final.Citations()
		if len(content.citations) > 0 {
			if !citationSlicesEqual(content.citations, finalCitations) {
				return acceptOutcome{}, m.error(CodeCompletionMismatch, "event.content_completed")
			}
		} else {
			atomicCitationBytes := citationSliceSize(finalCitations)
			atomicSize += atomicCitationBytes
			semantic = semantic || atomicCitationBytes > 0
		}
		if len(content.buffer) > 0 {
			if string(content.buffer) != final.Text() {
				return acceptOutcome{}, m.error(CodeCompletionMismatch, "event.content_completed")
			}
		} else {
			atomicSize += len(final.Text())
		}
	case canonical.ContentType("json"):
		final, _ := payload.Content().JSON()
		if len(content.buffer) > 0 {
			parsed, err := canonicaljson.Parse(content.buffer, canonicaljson.Limits{MaximumBytes: canonical.MaxJSONBytes, MaximumDepth: canonical.MaxJSONDepth})
			if err != nil || !parsed.Equal(final.Value()) {
				return acceptOutcome{}, m.error(CodeCompletionMismatch, "event.content_completed")
			}
		} else {
			atomicSize = len(final.Value().Bytes())
		}
	case canonical.ContentType("image"):
		if len(content.buffer) != 0 {
			return acceptOutcome{}, m.error(CodeCompletionMismatch, "event.content_completed")
		}
		atomicSize = contentSize(payload.Content())
	default:
		return acceptOutcome{}, m.error(CodeInvalidContentState, "event.content_completed")
	}
	if m.reconstructedBytes+atomicSize > m.options.MaxResponseBytes {
		return acceptOutcome{}, m.error(CodeResponseLimitExceeded, "event.content_completed")
	}
	copyContent.completed = true
	copyContent.finalContent = payload.Content()
	m.items[payload.ItemID()] = copyItem
	m.reconstructedBytes += atomicSize
	return acceptOutcome{semantic: semantic}, nil
}

func (m *Machine) acceptItemCompleted(event canonical.Event) (acceptOutcome, error) {
	payload, ok := event.ItemCompleted()
	if !ok {
		return acceptOutcome{}, m.error(CodeInvalidItemState, "event.item_completed")
	}
	item := m.items[payload.Item().ID()]
	if item == nil || payload.Index() != item.index {
		return acceptOutcome{}, m.error(CodeInvalidItemState, "event.item_completed")
	}
	if item.kind == canonical.ItemType("custom_tool_call") && !item.customInputCompleted {
		return acceptOutcome{}, m.error(CodeInvalidItemState, "event.item_completed")
	}
	reconstructed, atomicBytes, err := reconstructItem(item, payload.Item())
	if err != nil {
		return acceptOutcome{}, m.wrap(err)
	}
	if m.reconstructedBytes+atomicBytes > m.options.MaxResponseBytes {
		return acceptOutcome{}, m.error(CodeResponseLimitExceeded, "event.item_completed")
	}
	copyItem := item.clone()
	copyItem.completed = true
	copyItem.finalItem = reconstructed
	m.items[item.id] = copyItem
	m.reconstructedBytes += atomicBytes
	return acceptOutcome{semantic: item.kind == canonical.ItemType("tool_call") && len(item.arguments) == 0}, nil
}

func (m *Machine) acceptUsage(event canonical.Event) (acceptOutcome, error) {
	payload, ok := event.UsageUpdated()
	if !ok || !payload.Usage().Valid() {
		return acceptOutcome{}, m.error(CodeUsageRegression, "event.usage")
	}
	if !payload.Usage().AtLeast(m.latestUsage) {
		return acceptOutcome{}, m.error(CodeUsageRegression, "event.usage")
	}
	usage := payload.Usage()
	m.latestUsage = usage
	return acceptOutcome{usage: &usage}, nil
}

func (m *Machine) wrap(err error) error {
	var streamErr *Error
	if errors.As(err, &streamErr) {
		streamErr.Phase = m.phase()
		return streamErr
	}
	return &Error{Code: CodeCompletionMismatch, Path: "event", Phase: m.phase(), Err: errInvalidTransition}
}
