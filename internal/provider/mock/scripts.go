package mock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/execution"
)

const mockText = "mock response"
const mockReasoning = "mock reasoning summary"
const visionDigestHexLength = 12

func buildScript(ids IDGenerator, request canonical.Request) ([]canonical.Event, error, error) {
	responseID, err := ids.ResponseID()
	if err != nil {
		return nil, nil, err
	}
	builder := scriptBuilder{ids: ids, responseID: responseID, model: request.Model()}
	if err := builder.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewResponseStartedEvent(id, responseID, sequence, request.Model())
	}); err != nil {
		return nil, nil, err
	}
	if request.Model() == "mock/disconnect" {
		finalErr := execution.NewError(execution.CodeUnavailable, "mock.stream", 0, errors.New("mock stream disconnected"))
		return builder.events, finalErr, nil
	}
	switch request.Model() {
	case "mock/text", "mock/slow":
		err = builder.addText(mockText)
	case "mock/json":
		err = builder.addJSON(`{}`)
	case "mock/reasoning":
		err = builder.addReasoning(mockReasoning)
	case "mock/tool":
		if len(request.Tools()) == 0 {
			return nil, nil, errors.New("tool scenario requires a tool")
		}
		err = builder.addTool(request.Tools()[0].Name(), `{}`)
	case "mock/vision":
		err = builder.addText(visionSummary(request))
	default:
		return nil, nil, errors.New("unknown mock scenario")
	}
	if err != nil {
		return nil, nil, err
	}
	return builder.events, nil, nil
}

func requestImageCount(request canonical.Request) int {
	count := 0
	for _, item := range request.Input() {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, block := range message.Content() {
			if _, ok := block.Image(); ok {
				count++
			}
		}
	}
	return count
}

func visionSummary(request canonical.Request) string {
	lines := []string{fmt.Sprintf("images=%d", requestImageCount(request))}
	index := 0
	for _, item := range request.Input() {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, block := range message.Content() {
			image, ok := block.Image()
			if !ok {
				continue
			}
			source := "remote"
			mime := image.MIME()
			dimensions := "unknown"
			digest := ""
			if mime == "" {
				mime = "unknown"
			}
			if image.Width() > 0 && image.Height() > 0 {
				dimensions = fmt.Sprintf("%dx%d", image.Width(), image.Height())
			}
			if data, inline := image.Bytes(); inline {
				source = "inline"
				sum := sha256.Sum256(data)
				digest = " sha256=" + hex.EncodeToString(sum[:])[:visionDigestHexLength]
			}
			lines = append(lines, fmt.Sprintf("image[%d]: source=%s mime=%s detail=%s dimensions=%s%s", index, source, mime, image.Detail(), dimensions, digest))
			index++
		}
	}
	return strings.Join(lines, "\n")
}

type scriptBuilder struct {
	ids        IDGenerator
	responseID canonical.ResponseID
	model      string
	sequence   uint64
	events     []canonical.Event
}

type eventFactory func(canonical.EventID, uint64) (canonical.Event, error)

func (b *scriptBuilder) add(factory eventFactory) error {
	id, err := b.ids.EventID()
	if err != nil {
		return err
	}
	event, err := factory(id, b.sequence)
	if err != nil {
		return err
	}
	b.events = append(b.events, event)
	b.sequence++
	return nil
}

func (b *scriptBuilder) itemID() (canonical.ItemID, error) { return b.ids.ItemID() }

func (b *scriptBuilder) addText(text string) error {
	itemID, err := b.itemID()
	if err != nil {
		return err
	}
	content, err := canonical.NewTextContent(text)
	if err != nil {
		return err
	}
	block, err := canonical.NewTextBlock(content)
	if err != nil {
		return err
	}
	message, err := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	if err != nil {
		return err
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemStartedEvent(id, b.responseID, sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContentStartedEvent(id, b.responseID, sequence, itemID, 0, canonical.ContentType("text"))
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewTextDeltaEvent(id, b.responseID, sequence, itemID, 0, text)
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContentCompletedEvent(id, b.responseID, sequence, itemID, 0, block)
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemCompletedEvent(id, b.responseID, sequence, 0, item)
	}); err != nil {
		return err
	}
	return b.addTerminal(canonical.FinishReason("stop"), itemID, 1)
}

func (b *scriptBuilder) addJSON(value string) error {
	itemID, err := b.itemID()
	if err != nil {
		return err
	}
	jsonValue, err := canonicaljson.Parse([]byte(value), canonicaljson.DefaultLimits())
	if err != nil {
		return err
	}
	content, err := canonical.NewJSONContent(jsonValue)
	if err != nil {
		return err
	}
	block, err := canonical.NewJSONBlock(content)
	if err != nil {
		return err
	}
	message, err := canonical.NewMessage(itemID, canonical.Role("assistant"), []canonical.ContentBlock{block})
	if err != nil {
		return err
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemStartedEvent(id, b.responseID, sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("message"), Role: canonical.Role("assistant")})
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContentStartedEvent(id, b.responseID, sequence, itemID, 0, canonical.ContentType("json"))
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewJSONDeltaEvent(id, b.responseID, sequence, itemID, 0, value)
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContentCompletedEvent(id, b.responseID, sequence, itemID, 0, block)
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemCompletedEvent(id, b.responseID, sequence, 0, item)
	}); err != nil {
		return err
	}
	return b.addTerminal(canonical.FinishReason("stop"), itemID, 1)
}

func (b *scriptBuilder) addReasoning(text string) error {
	itemID, err := b.itemID()
	if err != nil {
		return err
	}
	content, err := canonical.NewTextContent(text)
	if err != nil {
		return err
	}
	block, err := canonical.NewTextBlock(content)
	if err != nil {
		return err
	}
	reasoning, err := canonical.NewReasoningSummary(itemID, []canonical.TextContent{content})
	if err != nil {
		return err
	}
	item, err := canonical.NewReasoningSummaryItem(reasoning)
	if err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemStartedEvent(id, b.responseID, sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("reasoning_summary")})
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContentStartedEvent(id, b.responseID, sequence, itemID, 0, canonical.ContentType("text"))
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewTextDeltaEvent(id, b.responseID, sequence, itemID, 0, text)
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewContentCompletedEvent(id, b.responseID, sequence, itemID, 0, block)
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemCompletedEvent(id, b.responseID, sequence, 0, item)
	}); err != nil {
		return err
	}
	return b.addTerminal(canonical.FinishReason("stop"), itemID, 2)
}

func (b *scriptBuilder) addTool(name, argumentsText string) error {
	itemID, err := b.itemID()
	if err != nil {
		return err
	}
	callID, err := b.ids.ToolCallID()
	if err != nil {
		return err
	}
	arguments, err := canonicaljson.Parse([]byte(argumentsText), canonicaljson.DefaultLimits())
	if err != nil {
		return err
	}
	call, err := canonical.NewToolCall(itemID, callID, name, arguments)
	if err != nil {
		return err
	}
	item, err := canonical.NewToolCallItem(call)
	if err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemStartedEvent(id, b.responseID, sequence, canonical.ItemStartParams{Index: 0, ID: itemID, Type: canonical.ItemType("tool_call"), CallID: callID, Name: name})
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewToolArgumentsDeltaEvent(id, b.responseID, sequence, itemID, argumentsText)
	}); err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewItemCompletedEvent(id, b.responseID, sequence, 0, item)
	}); err != nil {
		return err
	}
	return b.addTerminal(canonical.FinishReason("tool_call"), itemID, 1)
}

func (b *scriptBuilder) addTerminal(finish canonical.FinishReason, itemID canonical.ItemID, outputTokens int64) error {
	reasoningTokens := int64(0)
	if b.model == "mock/reasoning" {
		reasoningTokens = 1
	}
	usage, err := canonical.NewUsage(canonical.UsageParams{InputTokens: 1, OutputTokens: outputTokens, ReasoningTokens: reasoningTokens, TotalTokens: 1 + outputTokens})
	if err != nil {
		return err
	}
	if err := b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewUsageUpdatedEvent(id, b.responseID, sequence, usage)
	}); err != nil {
		return err
	}
	return b.add(func(id canonical.EventID, sequence uint64) (canonical.Event, error) {
		return canonical.NewResponseCompletedEvent(id, b.responseID, sequence, finish, usage, []canonical.ItemID{itemID})
	})
}
