package anthropic

import (
	"github.com/abyssbugg/ant-router/internal/canonical"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func (s *eventSource) mapMessageDelta(event sdk.MessageDeltaEvent) (canonical.Event, bool, error) {
	if !s.started || s.hasFinish || !allBlocksCompleted(s.blocks) {
		return canonical.Event{}, false, streamError("stream.message_delta")
	}
	finish, err := mapStopReason(event.Delta.StopReason)
	if err != nil {
		return canonical.Event{}, false, err
	}
	if event.Usage.OutputTokens < s.latestUsage.OutputTokens() {
		return canonical.Event{}, false, streamError("stream.usage")
	}
	usage, err := mapUsageFromPrevious(s.latestUsage, event.Usage)
	if err != nil || !usage.AtLeast(s.latestUsage) {
		return canonical.Event{}, false, streamError("stream.usage")
	}
	if usage == s.latestUsage {
		s.finish, s.hasFinish = finish, true
		return canonical.Event{}, false, nil
	}
	mapped, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewUsageUpdatedEvent(id, s.responseID, sequence, usage)
	})
	if err != nil {
		return canonical.Event{}, false, streamError("stream.canonical")
	}
	s.latestUsage, s.finish, s.hasFinish = usage, finish, true
	return mapped, true, nil
}

func (s *eventSource) mapMessageStop() (canonical.Event, bool, error) {
	if !s.started || !s.hasFinish || !allBlocksCompleted(s.blocks) {
		return canonical.Event{}, false, streamError("stream.message_stop")
	}
	if !s.messageStarted {
		completed, err := s.responseCompletedEvent()
		return completed, err == nil, err
	}
	contents := make([]canonical.ContentBlock, s.nextMessageContent)
	for index := range contents {
		block, ok := s.messageContents[index]
		if !ok {
			return canonical.Event{}, false, streamError("stream.message")
		}
		contents[index] = block
	}
	message, err := canonical.NewMessage(s.messageID, canonical.Role("assistant"), contents)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.message")
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.message")
	}
	itemEvent, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemCompletedEvent(id, s.responseID, sequence, s.messageIndex, item)
	})
	if err != nil {
		return canonical.Event{}, false, streamError("stream.canonical")
	}
	completed, err := s.responseCompletedEvent()
	if err != nil {
		return canonical.Event{}, false, err
	}
	s.pending = append(s.pending, completed)
	return itemEvent, true, nil
}

func (s *eventSource) responseCompletedEvent() (canonical.Event, error) {
	return s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewResponseCompletedEvent(id, s.responseID, sequence, s.finish, s.latestUsage, s.orderedItemIDs)
	})
}

func mapStopReason(reason sdk.StopReason) (canonical.FinishReason, error) {
	switch reason {
	case "end_turn", "stop_sequence":
		return canonical.FinishReason("stop"), nil
	case "max_tokens", "model_context_window_exceeded":
		return canonical.FinishReason("length"), nil
	case "tool_use":
		return canonical.FinishReason("tool_call"), nil
	case "refusal":
		return canonical.FinishReason("refusal"), nil
	default:
		return canonical.FinishReason(""), streamError("stream.stop_reason")
	}
}

func mapUsage(initial sdk.Usage, delta sdk.MessageDeltaUsage) (canonical.Usage, error) {
	input := initial.InputTokens + initial.CacheReadInputTokens + initial.CacheCreationInputTokens
	output := initial.OutputTokens
	if delta.InputTokens > 0 {
		input = delta.InputTokens + delta.CacheReadInputTokens + delta.CacheCreationInputTokens
	}
	if delta.OutputTokens > output {
		output = delta.OutputTokens
	}
	cacheRead, cacheWrite := initial.CacheReadInputTokens, initial.CacheCreationInputTokens
	if delta.CacheReadInputTokens > 0 {
		cacheRead = delta.CacheReadInputTokens
	}
	if delta.CacheCreationInputTokens > 0 {
		cacheWrite = delta.CacheCreationInputTokens
	}
	return canonical.NewUsage(canonical.UsageParams{InputTokens: input, OutputTokens: output, CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite, TotalTokens: input + output})
}

func mapUsageFromPrevious(previous canonical.Usage, delta sdk.MessageDeltaUsage) (canonical.Usage, error) {
	input, cacheRead, cacheWrite := previous.InputTokens(), previous.CacheReadTokens(), previous.CacheWriteTokens()
	if delta.InputTokens > 0 || delta.CacheReadInputTokens > 0 || delta.CacheCreationInputTokens > 0 {
		input = delta.InputTokens + delta.CacheReadInputTokens + delta.CacheCreationInputTokens
		cacheRead, cacheWrite = delta.CacheReadInputTokens, delta.CacheCreationInputTokens
	}
	output := previous.OutputTokens()
	if delta.OutputTokens > output {
		output = delta.OutputTokens
	}
	return canonical.NewUsage(canonical.UsageParams{InputTokens: input, OutputTokens: output, CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite, TotalTokens: input + output})
}

func allBlocksCompleted(blocks map[int]*blockState) bool {
	for _, block := range blocks {
		if !block.completed {
			return false
		}
	}
	return true
}
