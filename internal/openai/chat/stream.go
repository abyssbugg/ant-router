package chat

import (
	"crypto/rand"
	"encoding/json"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

// RandomIDs generates canonical identifiers from crypto/rand.
type RandomIDs struct{}

func (RandomIDs) ItemID() (canonical.ItemID, error) { return canonical.GenerateItemID(rand.Reader) }
func (RandomIDs) ToolCallID() (canonical.ToolCallID, error) {
	return canonical.GenerateToolCallID(rand.Reader)
}
func (RandomIDs) EventID() (canonical.EventID, error) { return canonical.GenerateEventID(rand.Reader) }

// Chunk is one streaming Chat Completions chunk. Unknown upstream fields are
// tolerated.
type Chunk struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *UsageWire    `json:"usage"`
	Error   *ErrorWire    `json:"error"`
}

type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

type ChunkDelta struct {
	Role             string         `json:"role"`
	Content          *string        `json:"content"`
	ReasoningContent *string        `json:"reasoning_content"`
	ToolCalls        []ToolCallWire `json:"tool_calls"`
}

// DecodeChunk parses one chat streaming chunk body.
func DecodeChunk(data []byte) (Chunk, error) {
	var chunk Chunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return Chunk{}, protocolErr("chunk.json")
	}
	if chunk.Error != nil {
		return Chunk{}, &Error{Code: "upstream_error", Path: "chunk.error", Message: "upstream returned an error chunk"}
	}
	return chunk, nil
}

type toolStreamState struct {
	itemID  canonical.ItemID
	callID  canonical.ToolCallID
	name    strings.Builder
	started bool
	args    strings.Builder
}

// Accumulator maps an ordered chat chunk stream onto canonical events. It
// enforces the canonical stream contract: dense item indices, contiguous
// content indices, byte-exact reconstruction, and monotonic usage.
type Accumulator struct {
	responseID  canonical.ResponseID
	publicModel string
	ids         IDGenerator
	sequence    uint64
	events      []canonical.Event

	started          bool
	nextItemIndex    int
	reasoningItemID  canonical.ItemID
	reasoningStarted bool
	reasoningBuffer  strings.Builder
	messageItemID    canonical.ItemID
	messageStarted   bool
	messageBuffer    strings.Builder
	tools            map[int]*toolStreamState
	toolOrder        []int
	finishReason     string
	usage            canonical.Usage
	orderedItemIDs   []canonical.ItemID
}

// NewAccumulator constructs one chunk accumulator for a single stream.
func NewAccumulator(responseID canonical.ResponseID, publicModel string, ids IDGenerator) *Accumulator {
	return &Accumulator{responseID: responseID, publicModel: publicModel, ids: ids, tools: make(map[int]*toolStreamState)}
}

// Accept consumes one chunk and returns the canonical events it produced.
func (a *Accumulator) Accept(chunk Chunk) ([]canonical.Event, error) {
	a.events = a.events[:0]
	if err := a.ensureStarted(); err != nil {
		return nil, err
	}
	if len(chunk.Choices) > 1 {
		return nil, protocolErr("chunk.choices")
	}
	if len(chunk.Choices) == 1 {
		choice := chunk.Choices[0]
		if err := a.acceptDelta(choice); err != nil {
			return nil, err
		}
		if choice.FinishReason != "" {
			a.finishReason = choice.FinishReason
		}
	}
	if chunk.Usage != nil {
		usage, err := canonical.NewUsage(UsageParams(*chunk.Usage))
		if err != nil {
			return nil, protocolErr("chunk.usage")
		}
		if err := a.emitUsage(usage); err != nil {
			return nil, err
		}
	}
	out := a.events
	a.events = nil
	return out, nil
}

// Finish closes the stream: flushes buffered reasoning, completes open
// items, and emits the terminal response_completed event.
func (a *Accumulator) Finish() ([]canonical.Event, error) {
	a.events = a.events[:0]
	if err := a.ensureStarted(); err != nil {
		return nil, err
	}
	if err := a.completeReasoning(); err != nil {
		return nil, err
	}
	if a.messageStarted {
		if err := a.completeMessage(); err != nil {
			return nil, err
		}
	}
	for _, index := range a.toolOrder {
		if err := a.completeTool(a.tools[index]); err != nil {
			return nil, err
		}
	}
	finish := a.finishReason
	if finish == "" {
		finish = "stop"
	}
	if err := a.emitTerminal(finish); err != nil {
		return nil, err
	}
	out := a.events
	a.events = nil
	return out, nil
}

func (a *Accumulator) ensureStarted() error {
	if a.started {
		return nil
	}
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	event, err := canonical.NewResponseStartedEvent(eventID, a.responseID, a.sequence, a.publicModel)
	if err != nil {
		return protocolErr("event.response_started")
	}
	a.sequence++
	a.started = true
	a.events = append(a.events, event)
	return nil
}

func (a *Accumulator) acceptDelta(choice ChunkChoice) error {
	delta := choice.Delta
	if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
		if err := a.acceptReasoningDelta(*delta.ReasoningContent); err != nil {
			return err
		}
	}
	if delta.Content != nil && *delta.Content != "" {
		if err := a.acceptMessageDelta(*delta.Content); err != nil {
			return err
		}
	}
	for _, call := range delta.ToolCalls {
		if err := a.acceptToolDelta(call); err != nil {
			return err
		}
	}
	return nil
}

func (a *Accumulator) startItem(start canonical.ItemStartParams) error {
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	start.Index = a.nextItemIndex
	event, err := canonical.NewItemStartedEvent(eventID, a.responseID, a.sequence, start)
	if err != nil {
		return protocolErr("event.item_started")
	}
	a.sequence++
	a.nextItemIndex++
	a.orderedItemIDs = append(a.orderedItemIDs, start.ID)
	a.events = append(a.events, event)
	return nil
}

// acceptReasoningDelta streams vendor reasoning text as a text content
// stream on a reasoning_summary item, matching the canonical contract that
// summaries reconstruct from streamed content.
func (a *Accumulator) acceptReasoningDelta(delta string) error {
	if !a.reasoningStarted {
		itemID, err := a.ids.ItemID()
		if err != nil {
			return protocolErr("event.id")
		}
		a.reasoningItemID = itemID
		if err := a.startItem(canonical.ItemStartParams{ID: itemID, Type: canonical.ItemType("reasoning_summary")}); err != nil {
			return err
		}
		contentID, err := a.ids.EventID()
		if err != nil {
			return protocolErr("event.id")
		}
		contentStarted, err := canonical.NewContentStartedEvent(contentID, a.responseID, a.sequence, a.reasoningItemID, 0, canonical.ContentType("text"))
		if err != nil {
			return protocolErr("event.content_started")
		}
		a.sequence++
		a.reasoningStarted = true
		a.events = append(a.events, contentStarted)
	}
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	textDelta, err := canonical.NewTextDeltaEvent(eventID, a.responseID, a.sequence, a.reasoningItemID, 0, delta)
	if err != nil {
		return protocolErr("event.text_delta")
	}
	a.sequence++
	a.reasoningBuffer.WriteString(delta)
	a.events = append(a.events, textDelta)
	return nil
}

func (a *Accumulator) completeReasoning() error {
	if !a.reasoningStarted {
		return nil
	}
	text, err := canonical.NewTextContent(a.reasoningBuffer.String())
	if err != nil {
		return protocolErr("reasoning.content")
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		return protocolErr("reasoning.block")
	}
	contentID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	contentCompleted, err := canonical.NewContentCompletedEvent(contentID, a.responseID, a.sequence, a.reasoningItemID, 0, block)
	if err != nil {
		return protocolErr("event.content_completed")
	}
	a.sequence++
	reasoning, err := canonical.NewReasoningSummary(a.reasoningItemID, []canonical.TextContent{text})
	if err != nil {
		return protocolErr("reasoning.summary")
	}
	item, err := canonical.NewReasoningSummaryItem(reasoning)
	if err != nil {
		return protocolErr("reasoning.item")
	}
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	completed, err := canonical.NewItemCompletedEvent(eventID, a.responseID, a.sequence, startIndexOf(a.orderedItemIDs, a.reasoningItemID), item)
	if err != nil {
		return protocolErr("event.item_completed")
	}
	a.sequence++
	a.reasoningStarted = false
	a.events = append(a.events, contentCompleted, completed)
	return nil
}

func (a *Accumulator) acceptMessageDelta(delta string) error {
	if !a.messageStarted {
		itemID, err := a.ids.ItemID()
		if err != nil {
			return protocolErr("event.id")
		}
		a.messageItemID = itemID
		if err := a.startItem(canonical.ItemStartParams{ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")}); err != nil {
			return err
		}
		contentID, err := a.ids.EventID()
		if err != nil {
			return protocolErr("event.id")
		}
		contentStarted, err := canonical.NewContentStartedEvent(contentID, a.responseID, a.sequence, a.messageItemID, 0, canonical.ContentType("text"))
		if err != nil {
			return protocolErr("event.content_started")
		}
		a.sequence++
		a.messageStarted = true
		a.events = append(a.events, contentStarted)
	}
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	textDelta, err := canonical.NewTextDeltaEvent(eventID, a.responseID, a.sequence, a.messageItemID, 0, delta)
	if err != nil {
		return protocolErr("event.text_delta")
	}
	a.sequence++
	a.messageBuffer.WriteString(delta)
	a.events = append(a.events, textDelta)
	return nil
}

func (a *Accumulator) completeMessage() error {
	text, err := canonical.NewTextContent(a.messageBuffer.String())
	if err != nil {
		return protocolErr("message.content")
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		return protocolErr("message.block")
	}
	contentID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	contentCompleted, err := canonical.NewContentCompletedEvent(contentID, a.responseID, a.sequence, a.messageItemID, 0, block)
	if err != nil {
		return protocolErr("event.content_completed")
	}
	a.sequence++
	message, err := canonical.NewMessage(a.messageItemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	if err != nil {
		return protocolErr("message.item")
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		return protocolErr("message.item")
	}
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	itemCompleted, err := canonical.NewItemCompletedEvent(eventID, a.responseID, a.sequence, startIndexOf(a.orderedItemIDs, a.messageItemID), item)
	if err != nil {
		return protocolErr("event.item_completed")
	}
	a.sequence++
	a.messageStarted = false
	a.events = append(a.events, contentCompleted, itemCompleted)
	return nil
}

func (a *Accumulator) acceptToolDelta(call ToolCallWire) error {
	state, exists := a.tools[call.Index]
	if !exists {
		if len(a.tools) >= canonical.MaxResponseItems {
			return protocolErr("tool_calls.limit")
		}
		itemID, err := a.ids.ItemID()
		if err != nil {
			return protocolErr("event.id")
		}
		callID, err := a.ids.ToolCallID()
		if err != nil {
			return protocolErr("event.id")
		}
		state = &toolStreamState{itemID: itemID, callID: callID}
		a.tools[call.Index] = state
		a.toolOrder = append(a.toolOrder, call.Index)
	}
	// Some compatible endpoints split the function name across chunks. Name
	// fragments accumulate until the first arguments fragment (or the call's
	// completion) starts the item; a name fragment after that is malformed.
	if call.Function.Name != "" {
		if state.started || state.name.Len()+len(call.Function.Name) > canonical.MaxToolNameBytes {
			return protocolErr("tool.name")
		}
		state.name.WriteString(call.Function.Name)
	}
	if !state.started {
		if state.name.Len() == 0 || call.Function.Arguments == "" {
			// Nothing has been emitted yet, so the streaming machine cannot
			// enforce its argument limit; bound the replay buffer here.
			if state.args.Len()+len(call.Function.Arguments) > canonical.MaxJSONBytes {
				return protocolErr("tool.arguments_limit")
			}
			state.args.WriteString(call.Function.Arguments)
			return nil
		}
		if err := a.startTool(state); err != nil {
			return err
		}
	}
	if call.Function.Arguments != "" {
		if err := a.emitToolArguments(state, call.Function.Arguments); err != nil {
			return err
		}
	}
	return nil
}

func (a *Accumulator) emitToolArguments(state *toolStreamState, delta string) error {
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	deltaEvent, err := canonical.NewToolArgumentsDeltaEvent(eventID, a.responseID, a.sequence, state.itemID, delta)
	if err != nil {
		return protocolErr("event.tool_arguments_delta")
	}
	a.sequence++
	state.args.WriteString(delta)
	a.events = append(a.events, deltaEvent)
	return nil
}

// startTool opens the canonical item for a call whose name is complete and
// replays argument fragments buffered before the start.
func (a *Accumulator) startTool(state *toolStreamState) error {
	if err := a.startItem(canonical.ItemStartParams{ID: state.itemID, Type: canonical.ItemType("tool_call"), CallID: state.callID, Name: state.name.String()}); err != nil {
		return err
	}
	state.started = true
	if state.args.Len() == 0 {
		return nil
	}
	buffered := state.args.String()
	state.args.Reset()
	return a.emitToolArguments(state, buffered)
}

func (a *Accumulator) completeTool(state *toolStreamState) error {
	if !state.started {
		if state.name.Len() == 0 {
			return protocolErr("tool.name")
		}
		if err := a.startTool(state); err != nil {
			return err
		}
	}
	arguments, err := canonicaljson.Parse([]byte(state.args.String()), canonicaljson.DefaultLimits())
	if err != nil {
		return protocolErr("tool.arguments")
	}
	toolCall, err := canonical.NewToolCall(state.itemID, state.callID, state.name.String(), arguments)
	if err != nil {
		return protocolErr("tool.item")
	}
	item, err := canonical.NewToolCallItem(toolCall)
	if err != nil {
		return protocolErr("tool.item")
	}
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	completed, err := canonical.NewItemCompletedEvent(eventID, a.responseID, a.sequence, startIndexOf(a.orderedItemIDs, state.itemID), item)
	if err != nil {
		return protocolErr("event.item_completed")
	}
	a.sequence++
	a.events = append(a.events, completed)
	return nil
}

func (a *Accumulator) emitUsage(usage canonical.Usage) error {
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	event, err := canonical.NewUsageUpdatedEvent(eventID, a.responseID, a.sequence, usage)
	if err != nil {
		return protocolErr("event.usage_updated")
	}
	a.sequence++
	a.usage = usage
	a.events = append(a.events, event)
	return nil
}

func (a *Accumulator) emitTerminal(finish string) error {
	eventID, err := a.ids.EventID()
	if err != nil {
		return protocolErr("event.id")
	}
	event, err := canonical.NewResponseCompletedEvent(eventID, a.responseID, a.sequence, FinishReason(finish), a.usage, a.orderedItemIDs)
	if err != nil {
		return protocolErr("event.response_completed")
	}
	a.sequence++
	a.events = append(a.events, event)
	return nil
}

func startIndexOf(order []canonical.ItemID, id canonical.ItemID) int {
	for index, value := range order {
		if value == id {
			return index
		}
	}
	return -1
}
