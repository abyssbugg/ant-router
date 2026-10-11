package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// SSEWriter is the minimal write-and-flush contract required by streaming responses.
type SSEWriter interface {
	io.Writer
	Flush()
}

// SSESink maps canonical events to OpenAI Responses SSE frames.
type SSESink struct {
	writer           SSEWriter
	encoder          Encoder
	machine          *streaming.Machine
	itemIndexes      map[canonical.ItemID]int
	itemTypes        map[canonical.ItemID]canonical.ItemType
	itemCallIDs      map[canonical.ItemID]canonical.ToolCallID
	reasoningEntries continuationOutputEntries
	model            string
}

// NewSSESink constructs a single-stream event sink.
func NewSSESink(writer SSEWriter, encoder Encoder) (*SSESink, error) {
	if nilSSEWriter(writer) {
		return nil, newError(CodeInternal, "stream.writer", errors.New("stream writer is unavailable"))
	}
	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		return nil, newError(CodeInternal, "stream.machine", errors.New("stream machine is unavailable"))
	}
	return &SSESink{writer: writer, encoder: encoder, machine: machine, itemIndexes: make(map[canonical.ItemID]int), itemTypes: make(map[canonical.ItemID]canonical.ItemType), itemCallIDs: make(map[canonical.ItemID]canonical.ToolCallID), reasoningEntries: make(continuationOutputEntries)}, nil
}

// Send validates one canonical event and writes its OpenAI SSE representation.
func (s *SSESink) Send(ctx context.Context, event canonical.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	transition, err := s.machine.Accept(event)
	if err != nil {
		return newError(CodeInternal, "stream.event", errors.New("canonical event sequence is invalid"))
	}
	eventType, payload, terminal, err := s.mapEvent(event, transition)
	if err != nil {
		return err
	}
	if eventType == "" {
		return nil
	}
	if err := s.writeFrame(ctx, eventType, payload); err != nil {
		return err
	}
	if terminal {
		if err := s.writeData(ctx, "data: [DONE]\n\n"); err != nil {
			return err
		}
	}
	return nil
}

func (s *SSESink) mapEvent(event canonical.Event, transition streaming.Transition) (string, any, bool, error) {
	base := map[string]any{"sequence_number": event.Sequence()}
	setType := func(value string) { base["type"] = value }
	switch event.Type() {
	case canonical.EventType("response_started"):
		payload, _ := event.ResponseStarted()
		s.model = payload.Model()
		setType("response.created")
		base["response"] = s.inProgressResponse(event.ResponseID(), payload.Model(), nil)
		return "response.created", base, false, nil
	case canonical.EventType("continuation_updated"):
		payload, _ := event.ContinuationUpdated()
		entries, err := continuationOutputIndex(payload.Continuation())
		if err != nil {
			return "", nil, false, newError(CodeInternal, "stream.continuation", errors.New("response continuation is invalid"))
		}
		s.reasoningEntries = entries
		return "", nil, false, nil
	case canonical.EventType("item_started"):
		payload, _ := event.ItemStarted()
		s.itemIndexes[payload.ID()] = payload.Index()
		s.itemTypes[payload.ID()] = payload.Type()
		if payload.Type() == canonical.ItemType("tool_call") {
			s.itemCallIDs[payload.ID()] = payload.CallID()
		}
		setType("response.output_item.added")
		base["output_index"] = payload.Index()
		item, err := startedOutputItem(payload, s.reasoningEntries)
		if err != nil {
			return "", nil, false, newError(CodeInternal, "stream.item", errors.New("started item is unsupported"))
		}
		base["item"] = item
		return "response.output_item.added", base, false, nil
	case canonical.EventType("content_started"):
		payload, _ := event.ContentStarted()
		name := "response.content_part.added"
		base["item_id"] = s.wireItemID(payload.ItemID())
		base["output_index"] = s.itemIndexes[payload.ItemID()]
		if s.itemTypes[payload.ItemID()] == canonical.ItemType("reasoning_summary") {
			name = "response.reasoning_summary_part.added"
			base["summary_index"] = payload.Index()
		} else {
			base["content_index"] = payload.Index()
		}
		setType(name)
		base["part"] = startedContentPart(s.itemTypes[payload.ItemID()], payload.Type())
		return name, base, false, nil
	case canonical.EventType("text_delta"):
		payload, _ := event.TextDelta()
		name := "response.output_text.delta"
		if s.itemTypes[payload.ItemID()] == canonical.ItemType("reasoning_summary") {
			name = "response.reasoning_summary_text.delta"
		}
		setType(name)
		base["item_id"] = s.wireItemID(payload.ItemID())
		base["output_index"] = s.itemIndexes[payload.ItemID()]
		if s.itemTypes[payload.ItemID()] == canonical.ItemType("reasoning_summary") {
			base["summary_index"] = payload.ContentIndex()
		} else {
			base["content_index"] = payload.ContentIndex()
		}
		base["delta"] = payload.Delta()
		return name, base, false, nil
	case canonical.EventType("json_delta"):
		payload, _ := event.JSONDelta()
		setType("response.output_text.delta")
		base["item_id"] = wireItemID("msg", payload.ItemID())
		base["output_index"] = s.itemIndexes[payload.ItemID()]
		base["content_index"] = payload.ContentIndex()
		base["delta"] = payload.Delta()
		return "response.output_text.delta", base, false, nil
	case canonical.EventType("tool_arguments_delta"):
		payload, _ := event.ToolArgumentsDelta()
		setType("response.function_call_arguments.delta")
		base["item_id"] = s.wireItemID(payload.ItemID())
		base["output_index"] = s.itemIndexes[payload.ItemID()]
		base["delta"] = payload.Delta()
		return "response.function_call_arguments.delta", base, false, nil
	case canonical.EventType("content_completed"):
		payload, _ := event.ContentCompleted()
		name := "response.output_text.done"
		if s.itemTypes[payload.ItemID()] == canonical.ItemType("reasoning_summary") {
			name = "response.reasoning_summary_text.done"
		}
		setType(name)
		base["item_id"] = s.wireItemID(payload.ItemID())
		base["output_index"] = s.itemIndexes[payload.ItemID()]
		if s.itemTypes[payload.ItemID()] == canonical.ItemType("reasoning_summary") {
			base["summary_index"] = payload.Index()
		} else {
			base["content_index"] = payload.Index()
		}
		base["text"] = contentText(payload.Content())
		return name, base, false, nil
	case canonical.EventType("item_completed"):
		payload, _ := event.ItemCompleted()
		item, err := encodeOutputItem(payload.Item(), s.reasoningEntries)
		if err != nil {
			return "", nil, false, newError(CodeInternal, "stream.item", errors.New("completed item is invalid"))
		}
		setType("response.output_item.done")
		base["output_index"] = payload.Index()
		base["item"] = item
		return "response.output_item.done", base, false, nil
	case canonical.EventType("usage_updated"):
		payload, _ := event.UsageUpdated()
		setType("response.in_progress")
		base["response"] = s.inProgressResponse(event.ResponseID(), s.model, &payload)
		return "response.in_progress", base, false, nil
	case canonical.EventType("response_completed"):
		response, ok := transition.Response()
		if !ok {
			return "", nil, false, newError(CodeInternal, "stream.response", errors.New("completed response is unavailable"))
		}
		encoded, err := s.encoder.EncodeResponse(response)
		if err != nil {
			return "", nil, false, err
		}
		name := "response.completed"
		if finish := response.FinishReason(); finish == canonical.FinishReason("length") || finish == canonical.FinishReason("content_filter") {
			name = "response.incomplete"
		}
		setType(name)
		base["response"] = json.RawMessage(encoded)
		return name, base, true, nil
	case canonical.EventType("response_failed"):
		payload, _ := event.ResponseFailed()
		setType("response.failed")
		base["response"] = s.terminalFailureResponse(event.ResponseID(), "failed", payload.Failure(), payload.Usage)
		return "response.failed", base, true, nil
	case canonical.EventType("response_cancelled"):
		payload, _ := event.ResponseCancelled()
		setType("response.incomplete")
		base["response"] = s.terminalFailureResponse(event.ResponseID(), "incomplete", payload.Failure(), payload.Usage)
		return "response.incomplete", base, true, nil
	default:
		return "", nil, false, newError(CodeInternal, "stream.event", errors.New("canonical event type is unsupported"))
	}
}

func (s *SSESink) inProgressResponse(id canonical.ResponseID, model string, updated *canonical.UsageUpdatedPayload) map[string]any {
	response := map[string]any{"id": wireResponseID(id), "object": "response", "created_at": s.encoder.createdAt, "status": "in_progress", "output": []any{}}
	if model != "" {
		response["model"] = model
	}
	if updated != nil {
		response["usage"] = encodeUsage(updated.Usage())
	}
	return response
}

func (s *SSESink) writeFrame(ctx context.Context, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return newError(CodeInternal, "stream.encode", errors.New("stream event encoding failed"))
	}
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, encoded)
	return s.writeData(ctx, frame)
}

func (s *SSESink) writeData(ctx context.Context, data string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	written, err := io.WriteString(s.writer, data)
	if err != nil || written != len(data) {
		return newError(CodeInternal, "stream.write", errors.New("stream write failed"))
	}
	s.writer.Flush()
	return nil
}

func startedOutputItem(payload canonical.ItemStartedPayload, continuationEntries continuationOutputEntries) (any, error) {
	switch payload.Type() {
	case canonical.ItemType("message"):
		return wireMessage{ID: wireItemID("msg", payload.ID()), Type: "message", Status: "in_progress", Role: "assistant", Content: []wireOutputText{}}, nil
	case canonical.ItemType("tool_call"):
		wireItem, wireCall := toolCallWireIdentity(payload.ID(), payload.CallID(), continuationEntries)
		return wireFunctionCall{ID: wireItem, Type: "function_call", Status: "in_progress", CallID: wireCall, Name: payload.Name(), Arguments: ""}, nil
	case canonical.ItemType("reasoning_summary"):
		wireID, encrypted := reasoningWireIdentity(payload.ID(), continuationEntries)
		return wireReasoning{ID: wireID, Type: "reasoning", Status: "in_progress", Summary: []wireReasoningSummary{}, EncryptedContent: encrypted}, nil
	default:
		return nil, errors.New("output item is unsupported")
	}
}

func startedContentPart(itemType canonical.ItemType, contentType canonical.ContentType) any {
	if itemType == canonical.ItemType("reasoning_summary") {
		return wireReasoningSummary{Type: "summary_text", Text: ""}
	}
	return wireOutputText{Type: "output_text", Text: "", Annotations: []any{}}
}

func contentText(content canonical.ContentBlock) string {
	if text, ok := content.Text(); ok {
		return text.Text()
	}
	if value, ok := content.JSON(); ok {
		return string(value.Value().Bytes())
	}
	return ""
}

func (s *SSESink) terminalFailureResponse(id canonical.ResponseID, status string, failure canonical.Failure, usage func() (canonical.Usage, bool)) map[string]any {
	response := map[string]any{"id": wireResponseID(id), "object": "response", "created_at": s.encoder.createdAt, "status": status, "model": s.model, "output": []any{}, "error": map[string]any{"code": string(failure.Code()), "message": failure.Message()}}
	if value, ok := usage(); ok {
		response["usage"] = encodeUsage(value)
	}
	return response
}

func (s *SSESink) wireItemID(itemID canonical.ItemID) string {
	itemType := s.itemTypes[itemID]
	if itemType == canonical.ItemType("reasoning_summary") {
		wireID, _ := reasoningWireIdentity(itemID, s.reasoningEntries)
		return wireID
	}
	if itemType == canonical.ItemType("tool_call") {
		wireID, _ := toolCallWireIdentity(itemID, s.itemCallIDs[itemID], s.reasoningEntries)
		return wireID
	}
	return wireItemID(itemPrefix(itemType), itemID)
}

func itemPrefix(itemType canonical.ItemType) string {
	switch itemType {
	case canonical.ItemType("tool_call"):
		return "fc"
	case canonical.ItemType("reasoning_summary"):
		return "rs"
	default:
		return "msg"
	}
}

func nilSSEWriter(writer SSEWriter) bool {
	if writer == nil {
		return true
	}
	value := reflect.ValueOf(writer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ streaming.EventSink = (*SSESink)(nil)
