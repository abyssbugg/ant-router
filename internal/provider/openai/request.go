package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/execution"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
	"github.com/openai/openai-go/v3/packages/param"
	sdkresponses "github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

func mapRequest(request canonical.Request, upstreamModel string) (sdkresponses.ResponseNewParams, error) {
	if !request.Valid() {
		return sdkresponses.ResponseNewParams{}, requestMapError("request")
	}
	if upstreamModel == "" {
		return sdkresponses.ResponseNewParams{}, requestMapError("provider.model")
	}
	if _, ok := request.Sampling().Seed(); ok {
		return sdkresponses.ResponseNewParams{}, requestMapError("sampling.seed")
	}

	replayByItem := make(map[string]openaicontinuation.ContinuationEntry)
	replayByCall := make(map[string]openaicontinuation.ContinuationEntry)
	hasContinuation := request.Continuation() != nil
	if continuation := request.Continuation(); continuation != nil {
		anchors := continuationItemIDs(request.Input())
		entries, err := openaicontinuation.DecodeContinuation(*continuation, continuation.Provider(), upstreamModel, anchors)
		if err != nil {
			return sdkresponses.ResponseNewParams{}, requestMapError("continuation")
		}
		for _, entry := range entries {
			replayByItem[entry.CanonicalItemID().String()] = entry
			if entry.IsFunctionCall() {
				replayByCall[entry.CanonicalCallID().String()] = entry
			}
		}
	}
	input, err := mapInput(request.Input(), replayByItem, replayByCall, hasContinuation)
	if err != nil {
		return sdkresponses.ResponseNewParams{}, err
	}
	tools, err := mapTools(request.Tools())
	if err != nil {
		return sdkresponses.ResponseNewParams{}, err
	}
	choice, err := mapToolChoice(request.ToolChoice())
	if err != nil {
		return sdkresponses.ResponseNewParams{}, err
	}
	text, err := mapOutput(request.Output())
	if err != nil {
		return sdkresponses.ResponseNewParams{}, err
	}

	result := sdkresponses.ResponseNewParams{
		Model:   shared.ResponsesModel(upstreamModel),
		Input:   sdkresponses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Tools:   tools,
		Text:    text,
		Store:   param.NewOpt(false),
		Include: []sdkresponses.ResponseIncludable{sdkresponses.ResponseIncludable("reasoning.encrypted_content")},
	}
	if len(tools) > 0 {
		result.ToolChoice = choice
	}
	output := request.Output()
	if output.MaxTokens() > 0 {
		result.MaxOutputTokens = param.NewOpt(int64(output.MaxTokens()))
	}
	if len(tools) > 0 || output.ParallelToolCalls() {
		result.ParallelToolCalls = param.NewOpt(output.ParallelToolCalls())
	}
	if value, ok := request.Sampling().Temperature(); ok {
		result.Temperature = param.NewOpt(value)
	}
	if value, ok := request.Sampling().TopP(); ok {
		result.TopP = param.NewOpt(value)
	}
	if mode := request.ReasoningSummary(); mode != canonical.ReasoningSummaryNone {
		result.Reasoning = shared.ReasoningParam{Summary: shared.ReasoningSummary(mode)}
	}
	return result, nil
}

func mapInput(items []canonical.Item, replayByItem, replayByCall map[string]openaicontinuation.ContinuationEntry, hasContinuation bool) (sdkresponses.ResponseInputParam, error) {
	result := make(sdkresponses.ResponseInputParam, 0, len(items))
	for index, item := range items {
		path := "input[" + jsonIndex(index) + "]"
		switch item.Type() {
		case canonical.ItemType("message"):
			message, _ := item.Message()
			content, err := mapMessageContent(message.Content(), path+".content")
			if err != nil {
				return nil, err
			}
			result = append(result, sdkresponses.ResponseInputItemParamOfMessage(content, sdkresponses.EasyInputMessageRole(message.Role())))
		case canonical.ItemType("tool_call"):
			call, _ := item.ToolCall()
			entry, exists := replayByItem[item.ID().String()]
			if hasContinuation && (!exists || !entry.IsFunctionCall() || entry.CanonicalCallID() != call.CallID()) {
				return nil, requestMapError(path + ".id")
			}
			mapped := sdkresponses.ResponseInputItemParamOfFunctionCall(string(call.Arguments().Bytes()), call.CallID().String(), call.Name())
			if exists {
				mapped.OfFunctionCall.ID = param.NewOpt(entry.UpstreamItemID())
				mapped.OfFunctionCall.CallID = entry.UpstreamCallID()
				mapped.OfFunctionCall.Status = sdkresponses.ResponseFunctionToolCallStatusCompleted
			}
			result = append(result, mapped)
		case canonical.ItemType("tool_result"):
			toolResult, _ := item.ToolResult()
			if toolResult.IsError() {
				return nil, requestMapError(path + ".is_error")
			}
			output, err := mapToolResultContent(toolResult.Content(), path+".content")
			if err != nil {
				return nil, err
			}
			callID := toolResult.CallID().String()
			if entry, exists := replayByCall[callID]; exists {
				callID = entry.UpstreamCallID()
			} else if hasContinuation {
				return nil, requestMapError(path + ".call_id")
			}
			result = append(result, sdkresponses.ResponseInputItemUnionParam{OfFunctionCallOutput: &sdkresponses.ResponseInputItemFunctionCallOutputParam{
				CallID: param.NewOpt(callID),
				Output: output,
			}})
		case canonical.ItemType("reasoning_summary"):
			reasoning, _ := item.ReasoningSummary()
			entry, exists := replayByItem[reasoning.ID().String()]
			if !exists || !entry.IsReasoning() {
				return nil, requestMapError(path + ".type")
			}
			summaries := reasoning.Summaries()
			mappedSummaries := make([]sdkresponses.ResponseReasoningItemSummaryParam, len(summaries))
			for summaryIndex, summary := range summaries {
				mappedSummaries[summaryIndex] = sdkresponses.ResponseReasoningItemSummaryParam{Text: summary.Text()}
			}
			mapped := sdkresponses.ResponseInputItemParamOfReasoning(entry.UpstreamItemID(), mappedSummaries)
			mapped.OfReasoning.EncryptedContent = param.NewOpt(entry.EncryptedContent())
			result = append(result, mapped)
		default:
			return nil, requestMapError(path + ".type")
		}
	}
	return result, nil
}

func continuationItemIDs(items []canonical.Item) []canonical.ItemID {
	ids := make([]canonical.ItemID, 0)
	for _, item := range items {
		if _, reasoning := item.ReasoningSummary(); reasoning {
			ids = append(ids, item.ID())
			continue
		}
		if _, toolCall := item.ToolCall(); toolCall {
			ids = append(ids, item.ID())
		}
	}
	return ids
}

func mapMessageContent(blocks []canonical.ContentBlock, path string) (sdkresponses.ResponseInputMessageContentListParam, error) {
	result := make(sdkresponses.ResponseInputMessageContentListParam, 0, len(blocks))
	for index, block := range blocks {
		mapped, err := mapInputContent(block, path+"["+jsonIndex(index)+"]")
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func mapInputContent(block canonical.ContentBlock, path string) (sdkresponses.ResponseInputContentUnionParam, error) {
	switch block.Type() {
	case canonical.ContentType("text"):
		value, _ := block.Text()
		return sdkresponses.ResponseInputContentParamOfInputText(value.Text()), nil
	case canonical.ContentType("json"):
		value, _ := block.JSON()
		return sdkresponses.ResponseInputContentParamOfInputText(string(value.Value().Bytes())), nil
	case canonical.ContentType("image"):
		image, _ := block.Image()
		mapped := sdkresponses.ResponseInputContentParamOfInputImage(sdkresponses.ResponseInputImageDetail(image.Detail()))
		if rawURL, ok := image.URL(); ok {
			mapped.OfInputImage.ImageURL = param.NewOpt(rawURL)
			return mapped, nil
		}
		data, ok := image.Bytes()
		if !ok {
			return sdkresponses.ResponseInputContentUnionParam{}, requestMapError(path)
		}
		mapped.OfInputImage.ImageURL = param.NewOpt("data:" + image.MIME() + ";base64," + base64.StdEncoding.EncodeToString(data))
		return mapped, nil
	default:
		return sdkresponses.ResponseInputContentUnionParam{}, requestMapError(path)
	}
}

func mapToolResultContent(blocks []canonical.ContentBlock, path string) (sdkresponses.ResponseInputItemFunctionCallOutputOutputUnionParam, error) {
	if len(blocks) == 1 {
		if text, ok := blocks[0].Text(); ok {
			return sdkresponses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: param.NewOpt(text.Text())}, nil
		}
		if value, ok := blocks[0].JSON(); ok {
			return sdkresponses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: param.NewOpt(string(value.Value().Bytes()))}, nil
		}
	}
	items := make(sdkresponses.ResponseFunctionCallOutputItemListParam, 0, len(blocks))
	for index, block := range blocks {
		itemPath := path + "[" + jsonIndex(index) + "]"
		if text, ok := block.Text(); ok {
			items = append(items, sdkresponses.ResponseFunctionCallOutputItemParamOfInputText(text.Text()))
			continue
		}
		if value, ok := block.JSON(); ok {
			items = append(items, sdkresponses.ResponseFunctionCallOutputItemParamOfInputText(string(value.Value().Bytes())))
			continue
		}
		image, ok := block.Image()
		if !ok {
			return sdkresponses.ResponseInputItemFunctionCallOutputOutputUnionParam{}, requestMapError(itemPath)
		}
		mapped, err := mapFunctionOutputImage(image, itemPath)
		if err != nil {
			return sdkresponses.ResponseInputItemFunctionCallOutputOutputUnionParam{}, err
		}
		items = append(items, mapped)
	}
	return sdkresponses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfResponseFunctionCallOutputItemArray: items}, nil
}

func mapFunctionOutputImage(image canonical.ImageContent, path string) (sdkresponses.ResponseFunctionCallOutputItemUnionParam, error) {
	content := &sdkresponses.ResponseInputImageContentParam{Detail: sdkresponses.ResponseInputImageContentDetail(image.Detail())}
	if rawURL, ok := image.URL(); ok {
		content.ImageURL = param.NewOpt(rawURL)
	} else if data, ok := image.Bytes(); ok {
		content.ImageURL = param.NewOpt("data:" + image.MIME() + ";base64," + base64.StdEncoding.EncodeToString(data))
	} else {
		return sdkresponses.ResponseFunctionCallOutputItemUnionParam{}, requestMapError(path)
	}
	return sdkresponses.ResponseFunctionCallOutputItemUnionParam{OfInputImage: content}, nil
}

func mapTools(tools []canonical.ToolDefinition) ([]sdkresponses.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	result := make([]sdkresponses.ToolUnionParam, 0, len(tools))
	for index, tool := range tools {
		schema, err := canonicalObject(tool.InputSchema())
		if err != nil {
			return nil, requestMapError("tools[" + jsonIndex(index) + "].parameters")
		}
		strict := tool.Enforcement() == canonical.SchemaEnforcementStrict
		mapped := &sdkresponses.FunctionToolParam{Name: tool.Name(), Parameters: schema, Strict: param.NewOpt(strict)}
		if tool.Description() != "" {
			mapped.Description = param.NewOpt(tool.Description())
		}
		result = append(result, sdkresponses.ToolUnionParam{OfFunction: mapped})
	}
	return result, nil
}

func mapToolChoice(choice canonical.ToolChoice) (sdkresponses.ResponseNewParamsToolChoiceUnion, error) {
	switch choice.Kind() {
	case canonical.ToolChoiceKind("auto"), canonical.ToolChoiceKind("none"), canonical.ToolChoiceKind("required"):
		return sdkresponses.ResponseNewParamsToolChoiceUnion{OfToolChoiceMode: param.NewOpt(sdkresponses.ToolChoiceOptions(choice.Kind()))}, nil
	case canonical.ToolChoiceKind("specific"):
		return sdkresponses.ResponseNewParamsToolChoiceUnion{OfFunctionTool: &sdkresponses.ToolChoiceFunctionParam{Name: choice.Name()}}, nil
	default:
		return sdkresponses.ResponseNewParamsToolChoiceUnion{}, requestMapError("tool_choice")
	}
}

func mapOutput(output canonical.OutputConstraints) (sdkresponses.ResponseTextConfigParam, error) {
	result := sdkresponses.ResponseTextConfigParam{}
	switch output.Format() {
	case canonical.OutputFormat("text"):
		return result, nil
	case canonical.OutputFormat("json_object"):
		result.Format = sdkresponses.ResponseFormatTextConfigUnionParam{OfJSONObject: &shared.ResponseFormatJSONObjectParam{}}
		return result, nil
	case canonical.OutputFormat("json_schema"):
		schema, err := canonicalObject(output.Schema())
		if err != nil {
			return sdkresponses.ResponseTextConfigParam{}, requestMapError("output.schema")
		}
		result.Format = sdkresponses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &sdkresponses.ResponseFormatTextJSONSchemaConfigParam{
			Name: output.SchemaName(), Schema: schema, Strict: param.NewOpt(output.Enforcement() == canonical.SchemaEnforcementStrict),
		}}
		return result, nil
	default:
		return sdkresponses.ResponseTextConfigParam{}, requestMapError("output.format")
	}
}

func canonicalObject(value canonicaljson.Value) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(value.Bytes()))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, errors.New("canonical object could not be decoded")
	}
	return preserveJSONNumbers(result).(map[string]any), nil
}

func preserveJSONNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		return json.RawMessage(typed.String())
	case map[string]any:
		for key, item := range typed {
			typed[key] = preserveJSONNumbers(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = preserveJSONNumbers(item)
		}
		return typed
	default:
		return value
	}
}

func jsonIndex(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[position:])
}

func requestMapError(path string) error {
	return execution.NewError(execution.CodeInvalidRequest, path, 0, errors.New("request cannot be mapped to provider"))
}
