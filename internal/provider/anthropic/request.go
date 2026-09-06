package anthropic

import (
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

func mapRequest(request canonical.Request, upstreamModel string, defaultMaxTokens, maxTokens int64) (sdk.MessageNewParams, error) {
	if !request.Valid() {
		return sdk.MessageNewParams{}, requestMapError("request")
	}
	if upstreamModel == "" {
		return sdk.MessageNewParams{}, requestMapError("provider.model")
	}
	if defaultMaxTokens <= 0 || maxTokens <= 0 || defaultMaxTokens > maxTokens || maxTokens > int64(canonical.MaxOutputTokens) {
		return sdk.MessageNewParams{}, requestMapError("provider.model.max_output_tokens")
	}
	if request.Continuation() != nil {
		return sdk.MessageNewParams{}, requestMapError("continuation")
	}
	if request.ReasoningSummary() != canonical.ReasoningSummaryNone {
		return sdk.MessageNewParams{}, requestMapError("reasoning_summary")
	}
	if _, ok := request.Sampling().Seed(); ok {
		return sdk.MessageNewParams{}, requestMapError("sampling.seed")
	}

	limit := int64(request.Output().MaxTokens())
	if limit == 0 {
		limit = defaultMaxTokens
	}
	if limit > maxTokens {
		return sdk.MessageNewParams{}, requestMapError("output.max_tokens")
	}

	system, messages, err := mapItems(request.Input())
	if err != nil {
		return sdk.MessageNewParams{}, err
	}
	tools, err := mapTools(request.Tools())
	if err != nil {
		return sdk.MessageNewParams{}, err
	}
	choice, err := mapToolChoice(request.ToolChoice(), request.Output().ParallelToolCalls())
	if err != nil {
		return sdk.MessageNewParams{}, err
	}
	output, err := mapOutput(request.Output())
	if err != nil {
		return sdk.MessageNewParams{}, err
	}

	result := sdk.MessageNewParams{
		Model: sdk.Model(upstreamModel), MaxTokens: limit, System: system,
		Messages: messages, Tools: tools, OutputConfig: output,
	}
	if len(tools) > 0 {
		result.ToolChoice = choice
	}
	if value, ok := request.Sampling().Temperature(); ok {
		result.Temperature = param.NewOpt(value)
	}
	if value, ok := request.Sampling().TopP(); ok {
		result.TopP = param.NewOpt(value)
	}
	return result, nil
}

func mapItems(items []canonical.Item) ([]sdk.TextBlockParam, []sdk.MessageParam, error) {
	var system []sdk.TextBlockParam
	var messages []sdk.MessageParam
	conversationStarted := false
	appendTurn := func(role sdk.MessageParamRole, blocks []sdk.ContentBlockParamUnion) {
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, blocks...)
			return
		}
		messages = append(messages, sdk.MessageParam{Role: role, Content: blocks})
	}

	for index, item := range items {
		path := "input[" + jsonIndex(index) + "]"
		if message, ok := item.Message(); ok {
			switch message.Role() {
			case canonical.Role("system"), canonical.Role("developer"):
				if conversationStarted {
					return nil, nil, requestMapError(path + ".role")
				}
				blocks, err := mapSystemContent(message.Content(), path+".content")
				if err != nil {
					return nil, nil, err
				}
				system = append(system, blocks...)
			case canonical.Role("user"), canonical.Role("assistant"):
				conversationStarted = true
				blocks, err := mapMessageContent(message.Content(), message.Role(), path+".content")
				if err != nil {
					return nil, nil, err
				}
				role := sdk.MessageParamRoleUser
				if message.Role() == canonical.Role("assistant") {
					role = sdk.MessageParamRoleAssistant
				}
				appendTurn(role, blocks)
			default:
				return nil, nil, requestMapError(path + ".role")
			}
			continue
		}
		conversationStarted = true
		if call, ok := item.ToolCall(); ok {
			input, err := canonicalAny(call.Arguments())
			if err != nil {
				return nil, nil, requestMapError(path + ".arguments")
			}
			appendTurn(sdk.MessageParamRoleAssistant, []sdk.ContentBlockParamUnion{sdk.NewToolUseBlock(call.CallID().String(), input, call.Name())})
			continue
		}
		if result, ok := item.ToolResult(); ok {
			block, err := mapToolResult(result, path)
			if err != nil {
				return nil, nil, err
			}
			appendTurn(sdk.MessageParamRoleUser, []sdk.ContentBlockParamUnion{block})
			continue
		}
		return nil, nil, requestMapError(path + ".type")
	}
	return system, messages, nil
}

func requestMapError(path string) error {
	return execution.NewError(execution.CodeInvalidRequest, path, 0, errors.New("request cannot be mapped to provider"))
}
