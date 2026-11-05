package chat

import (
	"context"
	"encoding/json"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// CompletionFinishReason maps a canonical finish reason to the chat wire.
func CompletionFinishReason(finish canonical.FinishReason) string {
	switch finish {
	case canonical.FinishReason("length"):
		return "length"
	case canonical.FinishReason("tool_call"):
		return "tool_calls"
	case canonical.FinishReason("content_filter"):
		return "content_filter"
	default:
		return "stop"
	}
}

func usageWireFromCanonical(usage canonical.Usage) UsageWire {
	return UsageWire{
		PromptTokens:     usage.InputTokens(),
		CompletionTokens: usage.OutputTokens(),
		TotalTokens:      usage.TotalTokens(),
		PromptTokensDetails: UsageDetails{
			CachedTokens: usage.CacheReadTokens(),
		},
	}
}

// EncodeCompletion renders one canonical response as a non-streaming Chat
// Completions completion object.
func EncodeCompletion(response canonical.Response, created int64) ([]byte, error) {
	if !response.Valid() {
		return nil, protocolErr("response")
	}
	choice := map[string]any{"index": 0, "finish_reason": CompletionFinishReason(response.FinishReason())}
	message := map[string]any{"role": "assistant"}
	text := ""
	toolCalls := make([]map[string]any, 0)
	for _, item := range response.Items() {
		if messageItem, ok := item.Message(); ok {
			for _, block := range messageItem.Content() {
				if value, ok := block.Text(); ok {
					if text != "" {
						text += "\n"
					}
					text += value.Text()
				} else if value, ok := block.JSON(); ok {
					if text != "" {
						text += "\n"
					}
					text += string(value.Value().Bytes())
				}
			}
		}
		if call, ok := item.ToolCall(); ok {
			toolCalls = append(toolCalls, map[string]any{
				"id":   string(call.CallID()),
				"type": "function",
				"function": map[string]any{
					"name":      call.Name(),
					"arguments": string(call.Arguments().Bytes()),
				},
			})
		}
	}
	if text != "" {
		message["content"] = text
	} else {
		message["content"] = nil
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	choice["message"] = message
	wire := map[string]any{
		"id":      string(response.ID()),
		"object":  "chat.completion",
		"created": created,
		"model":   response.Model(),
		"choices": []any{choice},
		"usage":   usageWireFromCanonical(response.Usage()),
	}
	return json.Marshal(wire)
}

// chunkSSEWriter is the write-and-flush contract for chat SSE frames.
type chunkSSEWriter interface {
	WriteChunk(data string) error
}

// SSEStreamSink maps canonical events onto Chat Completions SSE chunks. It
// re-validates every event through its own machine (defense in depth,
// mirroring the Responses SSE sink) and fails the stream rather than emitting
// a frame the chat dialect cannot express.
type SSEStreamSink struct {
	writer      chunkSSEWriter
	machine     *streaming.Machine
	completion  canonical.ResponseID
	model       string
	created     int64
	usage       canonical.Usage
	toolCounter int
	itemToolIdx map[canonical.ItemID]int
	itemTypes   map[canonical.ItemID]canonical.ItemType
}

// NewSSEStreamSink constructs one chat SSE stream sink.
func NewSSEStreamSink(writer chunkSSEWriter, model string, created int64) (*SSEStreamSink, error) {
	if writer == nil {
		return nil, protocolErr("stream.writer")
	}
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		return nil, protocolErr("stream.machine")
	}
	return &SSEStreamSink{
		writer:      writer,
		machine:     machine,
		model:       model,
		created:     created,
		itemToolIdx: make(map[canonical.ItemID]int),
		itemTypes:   make(map[canonical.ItemID]canonical.ItemType),
	}, nil
}

func chunkID(id canonical.ResponseID) string { return "chatcmpl-" + string(id) }

func (s *SSEStreamSink) frame(delta map[string]any, finishReason any) error {
	choice := map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}
	chunk := map[string]any{
		"id":      chunkID(s.completion),
		"object":  "chat.completion.chunk",
		"created": s.created,
		"model":   s.model,
		"choices": []any{choice},
	}
	encoded, err := json.Marshal(chunk)
	if err != nil {
		return protocolErr("stream.encode")
	}
	return s.writer.WriteChunk("data: " + string(encoded) + "\n\n")
}

func (s *SSEStreamSink) usageFrame() error {
	choice := map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": nil}
	chunk := map[string]any{
		"id":      chunkID(s.completion),
		"object":  "chat.completion.chunk",
		"created": s.created,
		"model":   s.model,
		"choices": []any{choice},
		"usage":   usageWireFromCanonical(s.usage),
	}
	encoded, err := json.Marshal(chunk)
	if err != nil {
		return protocolErr("stream.encode")
	}
	return s.writer.WriteChunk("data: " + string(encoded) + "\n\n")
}

// Send validates and maps one canonical event to chat SSE chunks.
func (s *SSEStreamSink) Send(ctx context.Context, event canonical.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	transition, err := s.machine.Accept(event)
	if err != nil {
		return protocolErr("stream.event")
	}
	switch event.Type() {
	case canonical.EventType("response_started"):
		payload, _ := event.ResponseStarted()
		s.model = payload.Model()
		s.completion = event.ResponseID()
		return s.frame(map[string]any{"role": "assistant"}, nil)
	case canonical.EventType("item_started"):
		payload, _ := event.ItemStarted()
		s.itemTypes[payload.ID()] = payload.Type()
		if payload.Type() == canonical.ItemType("tool_call") {
			index := s.toolCounter
			s.toolCounter++
			s.itemToolIdx[payload.ID()] = index
			return s.frame(map[string]any{
				"tool_calls": []map[string]any{{
					"index": index, "id": string(payload.CallID()), "type": "function",
					"function": map[string]any{"name": payload.Name(), "arguments": ""},
				}},
			}, nil)
		}
		return nil
	case canonical.EventType("content_started"):
		return nil
	case canonical.EventType("text_delta"), canonical.EventType("json_delta"):
		var itemID canonical.ItemID
		var delta string
		if payload, ok := event.TextDelta(); ok {
			itemID, delta = payload.ItemID(), payload.Delta()
		} else if payload, ok := event.JSONDelta(); ok {
			itemID, delta = payload.ItemID(), payload.Delta()
		} else {
			return protocolErr("stream.delta")
		}
		// The chat dialect has no distinct JSON delta frame: JSON content
		// streams through the regular content field.
		deltaMap := map[string]any{"content": delta}
		if s.itemTypes[itemID] == canonical.ItemType("reasoning_summary") {
			deltaMap = map[string]any{"reasoning_content": delta}
		}
		return s.frame(deltaMap, nil)
	case canonical.EventType("tool_arguments_delta"):
		payload, _ := event.ToolArgumentsDelta()
		index := s.itemToolIdx[payload.ItemID()]
		return s.frame(map[string]any{
			"tool_calls": []map[string]any{{"index": index, "function": map[string]any{"arguments": payload.Delta()}}},
		}, nil)
	case canonical.EventType("content_completed"), canonical.EventType("item_completed"):
		return nil
	case canonical.EventType("usage_updated"):
		payload, _ := event.UsageUpdated()
		s.usage = payload.Usage()
		return nil
	case canonical.EventType("response_completed"):
		response, ok := transition.Response()
		if !ok {
			return protocolErr("stream.response")
		}
		// The reconstructed response carries the authoritative usage; a
		// mid-stream usage_updated stash is only a fallback.
		s.usage = response.Usage()
		if err := s.frame(map[string]any{}, CompletionFinishReason(response.FinishReason())); err != nil {
			return err
		}
		if s.usage.TotalTokens() > 0 || s.usage.InputTokens() > 0 || s.usage.OutputTokens() > 0 {
			if err := s.usageFrame(); err != nil {
				return err
			}
		}
		return s.writer.WriteChunk("data: [DONE]\n\n")
	case canonical.EventType("response_failed"), canonical.EventType("response_cancelled"):
		// The chat dialect has no failure frame: fail the stream so the pump
		// reports the outcome and the handler can emit an HTTP error while
		// nothing has been written.
		return protocolErr("stream.terminal")
	case canonical.EventType("continuation_updated"):
		return nil
	default:
		return nil
	}
}

var _ streaming.EventSink = (*SSEStreamSink)(nil)
