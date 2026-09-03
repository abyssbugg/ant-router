package openai

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"reflect"
	"sync"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/execution"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
	"github.com/abyssbugg/ant-router/internal/streaming"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

type sdkEventStream interface {
	Next() bool
	Current() sdkresponses.ResponseStreamEventUnion
	Err() error
	Close() error
}

type streamIDGenerator interface {
	ResponseID() (canonical.ResponseID, error)
	EventID() (canonical.EventID, error)
	ItemID() (canonical.ItemID, error)
	ToolCallID() (canonical.ToolCallID, error)
}

type eventSource struct {
	stream        sdkEventStream
	publicModel   string
	providerID    string
	upstreamModel string
	format        canonical.OutputFormat
	ids           streamIDGenerator

	providerSequence    int64
	hasProviderEvent    bool
	canonicalSequence   uint64
	responseID          canonical.ResponseID
	upstreamResponse    string
	started             bool
	terminal            bool
	items               map[string]*sourceItem
	orderedItemIDs      []canonical.ItemID
	closeOnce           sync.Once
	closeErr            error
	fatalErr            error
	pending             []canonical.Event
	continuationEntries []openaicontinuation.ContinuationEntry
}

type sourceItem struct {
	id              canonical.ItemID
	index           int
	kind            canonical.ItemType
	contents        map[int]canonical.ContentBlock
	callID          canonical.ToolCallID
	upstreamCallID  string
	name            string
	arguments       string
	reasoningEvents []bufferedReasoningEvent
	reasoningBytes  int
	reasoningDone   bool
}

func newEventSource(stream sdkEventStream, publicModel string, format canonical.OutputFormat, ids streamIDGenerator) (*eventSource, error) {
	return newEventSourceWithAffinity(stream, publicModel, "openai-test", "gpt-test", format, ids)
}

func newEventSourceWithAffinity(stream sdkEventStream, publicModel, providerID, upstreamModel string, format canonical.OutputFormat, ids streamIDGenerator) (*eventSource, error) {
	if nilInterface(stream) || publicModel == "" || providerID == "" || upstreamModel == "" || !format.Valid() || nilInterface(ids) {
		return nil, streamError("stream.options")
	}
	return &eventSource{stream: stream, publicModel: publicModel, providerID: providerID, upstreamModel: upstreamModel, format: format, ids: ids, providerSequence: -1, items: make(map[string]*sourceItem)}, nil
}

func (s *eventSource) Recv(ctx context.Context) (canonical.Event, error) {
	if s == nil || ctx == nil {
		return canonical.Event{}, streamError("stream.source")
	}
	if err := ctx.Err(); err != nil {
		return canonical.Event{}, err
	}
	if s.fatalErr != nil {
		return canonical.Event{}, s.fatalErr
	}
	if s.terminal {
		return canonical.Event{}, io.EOF
	}
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		if !s.stream.Next() {
			if err := s.stream.Err(); err != nil {
				return canonical.Event{}, s.fail(normalizeProviderError(err))
			}
			return canonical.Event{}, io.EOF
		}
		if err := ctx.Err(); err != nil {
			return canonical.Event{}, err
		}
		upstream := s.stream.Current()
		if err := s.acceptProviderSequence(upstream.SequenceNumber); err != nil {
			return canonical.Event{}, s.fail(err)
		}
		event, emit, err := s.mapEvent(upstream)
		if err != nil {
			return canonical.Event{}, s.fail(err)
		}
		if emit {
			return event, nil
		}
	}
}

func (s *eventSource) fail(err error) error {
	if s.fatalErr == nil {
		s.fatalErr = err
	}
	return s.fatalErr
}

func (s *eventSource) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if err := s.stream.Close(); err != nil {
			s.closeErr = normalizeProviderError(err)
		}
	})
	return s.closeErr
}

func (s *eventSource) acceptProviderSequence(sequence int64) error {
	if sequence < 0 || (!s.hasProviderEvent && sequence != 0) || (s.hasProviderEvent && sequence != s.providerSequence+1) {
		return streamError("stream.sequence")
	}
	s.providerSequence = sequence
	s.hasProviderEvent = true
	return nil
}

func (s *eventSource) mapEvent(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	switch upstream.Type {
	case "response.created":
		if s.started || upstream.Response.ID == "" || upstream.Response.Status != "in_progress" {
			return canonical.Event{}, false, streamError("stream.response")
		}
		responseID, err := s.ids.ResponseID()
		if err != nil {
			return canonical.Event{}, false, streamError("stream.id")
		}
		event, err := s.newEvent(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
			return canonical.NewResponseStartedEvent(id, responseID, sequence, s.publicModel)
		})
		if err != nil {
			return canonical.Event{}, false, s.wrapCanonicalError(err)
		}
		s.responseID, s.upstreamResponse, s.started = responseID, upstream.Response.ID, true
		return event, true, nil
	case "response.in_progress", "response.queued":
		wantStatus := "in_progress"
		if upstream.Type == "response.queued" {
			wantStatus = "queued"
		}
		if !s.started || upstream.Response.ID != "" && upstream.Response.ID != s.upstreamResponse || string(upstream.Response.Status) != wantStatus {
			return canonical.Event{}, false, streamError("stream.response")
		}
		return canonical.Event{}, false, nil
	case "response.output_item.added":
		return s.mapItemStarted(upstream)
	case "response.content_part.added":
		return s.mapContentStarted(upstream)
	case "response.output_text.delta":
		return s.mapTextDelta(upstream)
	case "response.reasoning_summary_text.delta":
		return s.mapReasoningDelta(upstream)
	case "response.function_call_arguments.delta":
		return s.mapToolDelta(upstream)
	case "response.output_text.done":
		return s.mapContentCompleted(upstream)
	case "response.reasoning_summary_text.done":
		return s.mapReasoningCompleted(upstream)
	case "response.function_call_arguments.done":
		return s.mapToolArgumentsDone(upstream)
	case "response.content_part.done", "response.reasoning_summary_part.done":
		if err := s.validatePartDone(upstream); err != nil {
			return canonical.Event{}, false, err
		}
		return canonical.Event{}, false, nil
	case "response.reasoning_summary_part.added":
		return s.mapReasoningStarted(upstream)
	case "response.output_item.done":
		return s.mapItemCompleted(upstream)
	case "response.completed":
		if upstream.Response.Status != "completed" {
			return canonical.Event{}, false, streamError("stream.status")
		}
		finish := canonical.FinishReason("stop")
		for _, state := range s.items {
			if state.kind == canonical.ItemType("tool_call") {
				finish = canonical.FinishReason("tool_call")
			}
		}
		return s.mapResponseCompleted(upstream, finish)
	case "response.incomplete":
		if upstream.Response.Status != "incomplete" {
			return canonical.Event{}, false, streamError("stream.status")
		}
		switch upstream.Response.IncompleteDetails.Reason {
		case "max_output_tokens":
			return s.mapResponseCompleted(upstream, canonical.FinishReason("length"))
		case "content_filter":
			return s.mapResponseCompleted(upstream, canonical.FinishReason("content_filter"))
		default:
			return canonical.Event{}, false, streamError("stream.incomplete")
		}
	case "response.failed":
		if upstream.Response.Status != "failed" {
			return canonical.Event{}, false, streamError("stream.status")
		}
		return s.mapResponseFailed(upstream)
	case "error":
		return canonical.Event{}, false, streamError("stream.error")
	default:
		return canonical.Event{}, false, streamError("stream.event")
	}
}

func (s *eventSource) mapItemStarted(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	if !s.started || upstream.OutputIndex < 0 || upstream.OutputIndex >= int64(canonical.MaxResponseItems) || upstream.Item.ID == "" {
		return canonical.Event{}, false, streamError("stream.item")
	}
	if _, exists := s.items[upstream.Item.ID]; exists {
		return canonical.Event{}, false, streamError("stream.item")
	}
	itemID, err := s.ids.ItemID()
	if err != nil {
		return canonical.Event{}, false, streamError("stream.id")
	}
	state := &sourceItem{id: itemID, index: int(upstream.OutputIndex), contents: make(map[int]canonical.ContentBlock)}
	params := canonical.ItemStartParams{Index: state.index, ID: state.id}
	deferReasoning := false
	switch upstream.Item.Type {
	case "message":
		message := upstream.Item.AsMessage()
		if message.Role != "assistant" {
			return canonical.Event{}, false, streamError("stream.item.role")
		}
		state.kind = canonical.ItemType("message")
		params.Type, params.Role = state.kind, canonical.Role("assistant")
	case "function_call":
		call := upstream.Item.AsFunctionCall()
		if call.CallID == "" || call.Name == "" || call.Arguments != "" {
			return canonical.Event{}, false, streamError("stream.item.call")
		}
		callID, err := s.ids.ToolCallID()
		if err != nil {
			return canonical.Event{}, false, streamError("stream.id")
		}
		state.kind, state.callID, state.upstreamCallID, state.name = canonical.ItemType("tool_call"), callID, call.CallID, call.Name
		params.Type, params.CallID, params.Name = state.kind, callID, call.Name
	case "reasoning":
		state.kind = canonical.ItemType("reasoning_summary")
		params.Type = state.kind
		deferReasoning = true
	default:
		return canonical.Event{}, false, streamError("stream.item.type")
	}
	if deferReasoning {
		s.items[upstream.Item.ID] = state
		s.orderedItemIDs = append(s.orderedItemIDs, itemID)
		return canonical.Event{}, false, nil
	}
	event, err := s.newEvent(func(eventID canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemStartedEvent(eventID, s.responseID, sequence, params)
	})
	if err != nil {
		return canonical.Event{}, false, s.wrapCanonicalError(err)
	}
	s.items[upstream.Item.ID] = state
	s.orderedItemIDs = append(s.orderedItemIDs, itemID)
	return event, true, nil
}

func (s *eventSource) mapReasoningStarted(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil || state.kind != canonical.ItemType("reasoning_summary") || upstream.SummaryIndex < 0 || upstream.SummaryIndex >= int64(canonical.MaxContentBlocks) || upstream.Part.Type != "summary_text" {
		return canonical.Event{}, false, streamError("stream.reasoning")
	}
	if err := state.bufferReasoning(bufferedReasoningEvent{kind: bufferedReasoningStart, index: int(upstream.SummaryIndex)}, 0); err != nil {
		return canonical.Event{}, false, err
	}
	return canonical.Event{}, false, nil
}

func (s *eventSource) mapReasoningDelta(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil || state.kind != canonical.ItemType("reasoning_summary") || upstream.SummaryIndex < 0 || upstream.SummaryIndex >= int64(canonical.MaxContentBlocks) {
		return canonical.Event{}, false, streamError("stream.reasoning")
	}
	if err := state.bufferReasoning(bufferedReasoningEvent{kind: bufferedReasoningDelta, index: int(upstream.SummaryIndex), text: upstream.Delta}, len(upstream.Delta)); err != nil {
		return canonical.Event{}, false, err
	}
	return canonical.Event{}, false, nil
}

func (s *eventSource) mapReasoningCompleted(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil || state.kind != canonical.ItemType("reasoning_summary") || upstream.SummaryIndex < 0 || upstream.SummaryIndex >= int64(canonical.MaxContentBlocks) {
		return canonical.Event{}, false, streamError("stream.reasoning")
	}
	text, err := canonical.NewTextContent(upstream.Text)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.reasoning")
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.reasoning")
	}
	if err := state.bufferReasoning(bufferedReasoningEvent{kind: bufferedReasoningCompleted, index: int(upstream.SummaryIndex), block: block}, len(upstream.Text)); err != nil {
		return canonical.Event{}, false, err
	}
	state.contents[int(upstream.SummaryIndex)] = block
	return canonical.Event{}, false, nil
}

func (s *eventSource) mapToolDelta(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil || state.kind != canonical.ItemType("tool_call") || len(state.arguments) > canonical.MaxJSONBytes || len(upstream.Delta) > canonical.MaxJSONBytes-len(state.arguments) {
		return canonical.Event{}, false, streamError("stream.tool")
	}
	event, err := s.newEvent(func(eventID canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewToolArgumentsDeltaEvent(eventID, s.responseID, sequence, state.id, upstream.Delta)
	})
	if err != nil {
		return canonical.Event{}, false, s.wrapCanonicalError(err)
	}
	state.arguments += upstream.Delta
	return event, true, nil
}

func (s *eventSource) mapToolArgumentsDone(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil || state.kind != canonical.ItemType("tool_call") || upstream.Name != state.name || upstream.Arguments != state.arguments {
		return canonical.Event{}, false, streamError("stream.tool")
	}
	return canonical.Event{}, false, nil
}

func (s *eventSource) mapContentStarted(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil || upstream.ContentIndex < 0 || upstream.Part.Type != "output_text" {
		return canonical.Event{}, false, streamError("stream.content")
	}
	kind := canonical.ContentType("text")
	if s.format != canonical.OutputFormat("text") {
		kind = canonical.ContentType("json")
	}
	event, err := s.newEvent(func(eventID canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContentStartedEvent(eventID, s.responseID, sequence, state.id, int(upstream.ContentIndex), kind)
	})
	return event, true, s.wrapCanonicalError(err)
}

func (s *eventSource) mapTextDelta(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil || upstream.ContentIndex < 0 {
		return canonical.Event{}, false, streamError("stream.delta")
	}
	event, err := s.newEvent(func(eventID canonical.EventID, sequence uint64) (canonical.Event, error) {
		if s.format == canonical.OutputFormat("text") {
			return canonical.NewTextDeltaEvent(eventID, s.responseID, sequence, state.id, int(upstream.ContentIndex), upstream.Delta)
		}
		return canonical.NewJSONDeltaEvent(eventID, s.responseID, sequence, state.id, int(upstream.ContentIndex), upstream.Delta)
	})
	return event, true, s.wrapCanonicalError(err)
}

func (s *eventSource) mapContentCompleted(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil || upstream.ContentIndex < 0 {
		return canonical.Event{}, false, streamError("stream.content")
	}
	block, err := completedContent(s.format, upstream.Text)
	if err != nil {
		return canonical.Event{}, false, streamError("stream.content")
	}
	event, err := s.newEvent(func(eventID canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContentCompletedEvent(eventID, s.responseID, sequence, state.id, int(upstream.ContentIndex), block)
	})
	if err != nil {
		return canonical.Event{}, false, s.wrapCanonicalError(err)
	}
	state.contents[int(upstream.ContentIndex)] = block
	return event, true, nil
}

func (s *eventSource) validatePartDone(upstream sdkresponses.ResponseStreamEventUnion) error {
	state, err := s.item(upstream.ItemID, upstream.OutputIndex)
	if err != nil {
		return err
	}
	index := upstream.ContentIndex
	wantType := "output_text"
	if upstream.Type == "response.reasoning_summary_part.done" {
		index = upstream.SummaryIndex
		wantType = "summary_text"
		if state.kind != canonical.ItemType("reasoning_summary") {
			return streamError("stream.reasoning")
		}
	}
	if index < 0 || index >= int64(canonical.MaxContentBlocks) || upstream.Part.Type != wantType {
		return streamError("stream.content")
	}
	completed, ok := state.contents[int(index)]
	if !ok {
		return streamError("stream.content")
	}
	text, ok := completedText(completed)
	if !ok || upstream.Part.Text != text {
		return streamError("stream.content")
	}
	return nil
}

func completedText(block canonical.ContentBlock) (string, bool) {
	if text, ok := block.Text(); ok {
		return text.Text(), true
	}
	if value, ok := block.JSON(); ok {
		return string(value.Value().Bytes()), true
	}
	return "", false
}

func (s *eventSource) mapItemCompleted(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	state, err := s.item(upstream.Item.ID, upstream.OutputIndex)
	if err != nil || upstream.Item.Type != string(state.kind) && !(state.kind == canonical.ItemType("tool_call") && upstream.Item.Type == "function_call") && !(state.kind == canonical.ItemType("reasoning_summary") && upstream.Item.Type == "reasoning") {
		return canonical.Event{}, false, streamError("stream.item")
	}
	var item canonical.Item
	switch state.kind {
	case canonical.ItemType("message"):
		contents, err := orderedContents(state.contents)
		if err != nil {
			return canonical.Event{}, false, err
		}
		done := upstream.Item.AsMessage()
		if done.Status != "completed" || done.Role != "assistant" || len(done.Content) != len(contents) {
			return canonical.Event{}, false, streamError("stream.item")
		}
		for index, content := range contents {
			text, ok := completedText(content)
			if !ok || done.Content[index].Type != "output_text" || done.Content[index].Text != text {
				return canonical.Event{}, false, streamError("stream.item")
			}
		}
		message, err := canonical.NewMessage(state.id, canonical.Role("assistant"), contents)
		if err != nil {
			return canonical.Event{}, false, streamError("stream.item")
		}
		item, err = canonical.NewMessageItem(message)
		if err != nil {
			return canonical.Event{}, false, streamError("stream.item")
		}
	case canonical.ItemType("tool_call"):
		call := upstream.Item.AsFunctionCall()
		if call.Status != "completed" || call.CallID != state.upstreamCallID || call.Name != state.name || call.Arguments != state.arguments {
			return canonical.Event{}, false, streamError("stream.tool")
		}
		arguments, err := canonicaljson.Parse([]byte(state.arguments), canonicaljson.DefaultLimits())
		if err != nil {
			return canonical.Event{}, false, streamError("stream.tool")
		}
		toolCall, err := canonical.NewToolCall(state.id, state.callID, state.name, arguments)
		if err != nil {
			return canonical.Event{}, false, streamError("stream.tool")
		}
		item, err = canonical.NewToolCallItem(toolCall)
		if err != nil {
			return canonical.Event{}, false, streamError("stream.tool")
		}
	case canonical.ItemType("reasoning_summary"):
		contents, err := orderedContents(state.contents)
		if err != nil {
			return canonical.Event{}, false, err
		}
		done := upstream.Item.AsReasoning()
		if done.Status != "completed" || len(done.Summary) != len(contents) {
			return canonical.Event{}, false, streamError("stream.reasoning")
		}
		summaries := make([]canonical.TextContent, len(contents))
		for index, content := range contents {
			text, ok := content.Text()
			if !ok || done.Summary[index].Type != "summary_text" || done.Summary[index].Text != text.Text() {
				return canonical.Event{}, false, streamError("stream.reasoning")
			}
			summaries[index] = text
		}
		reasoning, err := canonical.NewReasoningSummary(state.id, summaries)
		if err != nil {
			return canonical.Event{}, false, streamError("stream.reasoning")
		}
		item, err = canonical.NewReasoningSummaryItem(reasoning)
		if err != nil {
			return canonical.Event{}, false, streamError("stream.reasoning")
		}
		return s.completeReasoning(state, upstream.Item.ID, done.EncryptedContent, item)
	default:
		return canonical.Event{}, false, streamError("stream.item.type")
	}
	event, err := s.newEvent(func(eventID canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemCompletedEvent(eventID, s.responseID, sequence, state.index, item)
	})
	return event, true, s.wrapCanonicalError(err)
}

func orderedContents(contents map[int]canonical.ContentBlock) ([]canonical.ContentBlock, error) {
	ordered := make([]canonical.ContentBlock, len(contents))
	for index := range ordered {
		content, exists := contents[index]
		if !exists {
			return nil, streamError("stream.item")
		}
		ordered[index] = content
	}
	return ordered, nil
}

func (s *eventSource) mapResponseCompleted(upstream sdkresponses.ResponseStreamEventUnion, finish canonical.FinishReason) (canonical.Event, bool, error) {
	if !s.started || upstream.Response.ID != s.upstreamResponse || s.hasUnfinishedReasoning() {
		return canonical.Event{}, false, streamError("stream.response")
	}
	usage, err := canonical.NewUsage(canonical.UsageParams{
		InputTokens: upstream.Response.Usage.InputTokens, OutputTokens: upstream.Response.Usage.OutputTokens,
		ReasoningTokens:  upstream.Response.Usage.OutputTokensDetails.ReasoningTokens,
		CacheReadTokens:  upstream.Response.Usage.InputTokensDetails.CachedTokens,
		CacheWriteTokens: upstream.Response.Usage.InputTokensDetails.CacheWriteTokens,
		TotalTokens:      upstream.Response.Usage.TotalTokens,
	})
	if err != nil {
		return canonical.Event{}, false, streamError("stream.usage")
	}
	event, err := s.newEvent(func(eventID canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewResponseCompletedEvent(eventID, s.responseID, sequence, finish, usage, s.orderedItemIDs)
	})
	if err == nil {
		s.terminal = true
	}
	return event, true, s.wrapCanonicalError(err)
}

func (s *eventSource) mapResponseFailed(upstream sdkresponses.ResponseStreamEventUnion) (canonical.Event, bool, error) {
	if !s.started || upstream.Response.ID != s.upstreamResponse {
		return canonical.Event{}, false, streamError("stream.response")
	}
	failure, err := canonical.NewFailure(canonical.FailureParams{
		Code: canonical.FailureCode("upstream_protocol_error"), Message: "OpenAI response failed", Retryable: false,
	})
	if err != nil {
		return canonical.Event{}, false, streamError("stream.failure")
	}
	event, err := s.newEvent(func(eventID canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewResponseFailedEvent(eventID, s.responseID, sequence, failure, nil)
	})
	if err == nil {
		s.terminal = true
	}
	return event, true, s.wrapCanonicalError(err)
}

func (s *eventSource) item(upstreamID string, outputIndex int64) (*sourceItem, error) {
	state, ok := s.items[upstreamID]
	if !ok || outputIndex < 0 || state.index != int(outputIndex) {
		return nil, streamError("stream.item")
	}
	return state, nil
}

func (s *eventSource) newEvent(build func(canonical.EventID, uint64) (canonical.Event, error)) (canonical.Event, error) {
	eventID, err := s.ids.EventID()
	if err != nil {
		return canonical.Event{}, err
	}
	event, err := build(eventID, s.canonicalSequence)
	if err == nil {
		s.canonicalSequence++
	}
	return event, err
}

func (s *eventSource) wrapCanonicalError(err error) error {
	if err == nil {
		return nil
	}
	return streamError("stream.canonical")
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
	jsonContent, err := canonical.NewJSONContent(value)
	if err != nil {
		return canonical.ContentBlock{}, err
	}
	return canonical.NewJSONBlock(jsonContent)
}

func streamError(path string) error {
	return execution.NewError(execution.CodeInternal, path, 0, errors.New("provider stream is invalid"))
}

type randomStreamIDs struct{}

func (randomStreamIDs) ResponseID() (canonical.ResponseID, error) {
	return canonical.GenerateResponseID(rand.Reader)
}
func (randomStreamIDs) EventID() (canonical.EventID, error) {
	return canonical.GenerateEventID(rand.Reader)
}
func (randomStreamIDs) ItemID() (canonical.ItemID, error) {
	return canonical.GenerateItemID(rand.Reader)
}
func (randomStreamIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(rand.Reader)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ streaming.EventSource = (*eventSource)(nil)
