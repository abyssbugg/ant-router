package anthropic

import (
	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func (s *eventSource) mapBlockStart(event sdk.ContentBlockStartEvent) (canonical.Event, bool, error) {
	if !s.started || s.hasFinish || event.Index < 0 || event.Index >= int64(canonical.MaxContentBlocks) || int(event.Index) != s.nextBlock {
		return canonical.Event{}, false, streamError("stream.content_block_start")
	}
	index := int(event.Index)
	switch event.ContentBlock.Type {
	case "text":
		if event.ContentBlock.Text != "" {
			return canonical.Event{}, false, streamError("stream.text")
		}
		return s.startTextBlock(index)
	case "tool_use":
		encoded, err := json.Marshal(event.ContentBlock.Input)
		if err != nil || string(encoded) != "{}" || event.ContentBlock.ID == "" || event.ContentBlock.Name == "" {
			return canonical.Event{}, false, streamError("stream.tool")
		}
		return s.startToolBlock(index, event.ContentBlock.Name)
	default:
		return canonical.Event{}, false, streamError("stream.content_block_start.type")
	}
}

func (s *eventSource) startTextBlock(blockIndex int) (canonical.Event, bool, error) {
	contentIndex := s.nextMessageContent
	state := &blockState{index: blockIndex, kind: "text", contentIndex: contentIndex}
	if !s.messageStarted {
		itemID, err := s.ids.ItemID()
		if err != nil {
			return canonical.Event{}, false, streamError("stream.id")
		}
		itemIndex := len(s.orderedItemIDs)
		itemEvent, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
			return canonical.NewItemStartedEvent(id, s.responseID, sequence, canonical.ItemStartParams{Index: itemIndex, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
		})
		if err != nil {
			return canonical.Event{}, false, streamError("stream.canonical")
		}
		contentEvent, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
			kind := canonical.ContentType("text")
			if s.format != canonical.OutputFormat("text") {
				kind = canonical.ContentType("json")
			}
			return canonical.NewContentStartedEvent(id, s.responseID, sequence, itemID, contentIndex, kind)
		})
		if err != nil {
			return canonical.Event{}, false, streamError("stream.canonical")
		}
		s.messageID, s.messageIndex, s.messageStarted = itemID, itemIndex, true
		s.orderedItemIDs = append(s.orderedItemIDs, itemID)
		state.itemID = itemID
		s.blocks[blockIndex], s.nextBlock, s.nextMessageContent = state, s.nextBlock+1, s.nextMessageContent+1
		s.pending = append(s.pending, contentEvent)
		return itemEvent, true, nil
	}
	state.itemID = s.messageID
	contentEvent, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		kind := canonical.ContentType("text")
		if s.format != canonical.OutputFormat("text") {
			kind = canonical.ContentType("json")
		}
		return canonical.NewContentStartedEvent(id, s.responseID, sequence, s.messageID, contentIndex, kind)
	})
	if err != nil {
		return canonical.Event{}, false, streamError("stream.canonical")
	}
	s.blocks[blockIndex], s.nextBlock, s.nextMessageContent = state, s.nextBlock+1, s.nextMessageContent+1
	return contentEvent, true, nil
}

func (s *eventSource) startToolBlock(blockIndex int, name string) (canonical.Event, bool, error) {
	itemID, err := s.ids.ItemID()
	if err != nil {
		return canonical.Event{}, false, streamError("stream.id")
	}
	callID, err := s.ids.ToolCallID()
	if err != nil {
		return canonical.Event{}, false, streamError("stream.id")
	}
	itemIndex := len(s.orderedItemIDs)
	event, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemStartedEvent(id, s.responseID, sequence, canonical.ItemStartParams{Index: itemIndex, ID: itemID, Type: canonical.ItemType("tool_call"), CallID: callID, Name: name})
	})
	if err != nil {
		return canonical.Event{}, false, streamError("stream.canonical")
	}
	s.blocks[blockIndex] = &blockState{index: blockIndex, kind: "tool", itemID: itemID, callID: callID, name: name}
	s.nextBlock++
	s.orderedItemIDs = append(s.orderedItemIDs, itemID)
	return event, true, nil
}

func (s *eventSource) mapBlockDelta(event sdk.ContentBlockDeltaEvent) (canonical.Event, bool, error) {
	if event.Index < 0 || event.Index >= int64(canonical.MaxContentBlocks) {
		return canonical.Event{}, false, streamError("stream.content_block_delta.index")
	}
	state, ok := s.blocks[int(event.Index)]
	if !ok || state.completed {
		return canonical.Event{}, false, streamError("stream.content_block_delta")
	}
	switch event.Delta.Type {
	case "text_delta":
		if state.kind != "text" {
			return canonical.Event{}, false, streamError("stream.text")
		}
		delta := event.Delta.AsTextDelta().Text
		if delta == "" || len(state.text) > canonical.MaxTextBytes-len(delta) {
			return canonical.Event{}, false, streamError("stream.text")
		}
		mapped, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
			if s.format == canonical.OutputFormat("text") {
				return canonical.NewTextDeltaEvent(id, s.responseID, sequence, state.itemID, state.contentIndex, delta)
			}
			return canonical.NewJSONDeltaEvent(id, s.responseID, sequence, state.itemID, state.contentIndex, delta)
		})
		if err != nil {
			return canonical.Event{}, false, streamError("stream.canonical")
		}
		state.text += delta
		return mapped, true, nil
	case "input_json_delta":
		if state.kind != "tool" {
			return canonical.Event{}, false, streamError("stream.tool")
		}
		delta := event.Delta.AsInputJSONDelta().PartialJSON
		if delta == "" || len(state.arguments) > canonical.MaxJSONBytes-len(delta) {
			return canonical.Event{}, false, streamError("stream.tool")
		}
		mapped, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
			return canonical.NewToolArgumentsDeltaEvent(id, s.responseID, sequence, state.itemID, delta)
		})
		if err != nil {
			return canonical.Event{}, false, streamError("stream.canonical")
		}
		state.arguments += delta
		return mapped, true, nil
	default:
		return canonical.Event{}, false, streamError("stream.content_block_delta.type")
	}
}

func (s *eventSource) mapBlockStop(event sdk.ContentBlockStopEvent) (canonical.Event, bool, error) {
	if event.Index < 0 || event.Index >= int64(canonical.MaxContentBlocks) {
		return canonical.Event{}, false, streamError("stream.content_block_stop.index")
	}
	state, ok := s.blocks[int(event.Index)]
	if !ok || state.completed {
		return canonical.Event{}, false, streamError("stream.content_block_stop")
	}
	if state.kind == "text" {
		block, err := completedContent(s.format, state.text)
		if err != nil {
			return canonical.Event{}, false, streamError("stream.content")
		}
		mapped, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
			return canonical.NewContentCompletedEvent(id, s.responseID, sequence, state.itemID, state.contentIndex, block)
		})
		if err != nil {
			return canonical.Event{}, false, streamError("stream.canonical")
		}
		state.completed = true
		s.messageContents[state.contentIndex] = block
		return mapped, true, nil
	}
	arguments, err := canonicaljson.Parse([]byte(state.arguments), canonicaljson.DefaultLimits())
	if err != nil {
		return canonical.Event{}, false, streamError("stream.tool")
	}
	call, err := canonical.NewToolCall(state.itemID, state.callID, state.name, arguments)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.tool")
	}
	item, err := canonical.NewToolCallItem(call)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.tool")
	}
	itemIndex := indexOfItem(s.orderedItemIDs, state.itemID)
	mapped, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemCompletedEvent(id, s.responseID, sequence, itemIndex, item)
	})
	if err != nil {
		return canonical.Event{}, false, streamError("stream.canonical")
	}
	state.completed = true
	return mapped, true, nil
}

func completedContent(format canonical.OutputFormat, text string) (canonical.ContentBlock, error) {
	if format == canonical.OutputFormat("text") {
		value, err := canonical.NewTextContent(text)
		if err != nil {
			return canonical.ContentBlock{}, err
		}
		return canonical.NewTextBlock(value)
	}
	value, err := canonicaljson.Parse([]byte(text), canonicaljson.DefaultLimits())
	if err != nil {
		return canonical.ContentBlock{}, err
	}
	content, err := canonical.NewJSONContent(value)
	if err != nil {
		return canonical.ContentBlock{}, err
	}
	return canonical.NewJSONBlock(content)
}

func indexOfItem(items []canonical.ItemID, target canonical.ItemID) int {
	for index, item := range items {
		if item == target {
			return index
		}
	}
	return -1
}
