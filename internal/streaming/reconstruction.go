package streaming

import (
	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

func reconstructItem(state *itemState, final canonical.Item) (canonical.Item, int, error) {
	if state == nil || state.completed || state.id != final.ID() || state.kind != final.Type() {
		return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
	}
	switch state.kind {
	case canonical.ItemType("message"):
		content, err := completedContent(state)
		if err != nil {
			return canonical.Item{}, 0, err
		}
		message, err := canonical.NewMessage(state.id, state.role, content)
		if err != nil {
			return canonical.Item{}, 0, err
		}
		item, err := canonical.NewMessageItem(message)
		if err != nil {
			return canonical.Item{}, 0, err
		}
		if !itemEqual(item, final) {
			return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
		}
		return item, 0, nil
	case canonical.ItemType("reasoning_summary"):
		var content []canonical.ContentBlock
		// Any streamed content state must pass normal completion validation;
		// only a completely absent summary is reconstructed as empty.
		if state.nextContent > 0 || len(state.contents) > 0 {
			var err error
			content, err = completedContent(state)
			if err != nil {
				return canonical.Item{}, 0, err
			}
		}
		summaries := make([]canonical.TextContent, 0, len(content))
		for _, block := range content {
			value, ok := block.Text()
			if !ok {
				return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
			}
			summaries = append(summaries, value)
		}
		reasoning, err := canonical.NewReasoningSummary(state.id, summaries)
		if err != nil {
			return canonical.Item{}, 0, err
		}
		item, err := canonical.NewReasoningSummaryItem(reasoning)
		if err != nil {
			return canonical.Item{}, 0, err
		}
		if !itemEqual(item, final) {
			return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
		}
		return item, 0, nil
	case canonical.ItemType("tool_call"):
		finalCall, ok := final.ToolCall()
		if !ok || finalCall.CallID() != state.callID || finalCall.Namespace() != state.namespace || finalCall.Name() != state.name || len(state.contents) != 0 {
			return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
		}
		arguments := finalCall.Arguments()
		atomicBytes := 0
		if len(state.arguments) > 0 {
			parsed, err := canonicaljson.Parse(state.arguments, canonicaljson.Limits{MaximumBytes: canonical.MaxJSONBytes, MaximumDepth: canonical.MaxJSONDepth})
			if err != nil || !parsed.IsObject() {
				return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
			}
			arguments = parsed
		} else {
			atomicBytes = len(arguments.Bytes())
		}
		var call canonical.ToolCall
		var err error
		if state.namespace == "" {
			call, err = canonical.NewToolCall(state.id, state.callID, state.name, arguments)
		} else {
			call, err = canonical.NewNamespacedToolCall(state.id, state.callID, state.namespace, state.name, arguments)
		}
		if err != nil {
			return canonical.Item{}, 0, err
		}
		item, err := canonical.NewToolCallItem(call)
		if err != nil {
			return canonical.Item{}, 0, err
		}
		if !itemEqual(item, final) {
			return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
		}
		return item, atomicBytes, nil
	case canonical.ItemType("custom_tool_call"):
		finalCall, ok := final.CustomToolCall()
		if !ok || !state.customInputCompleted || finalCall.CallID() != state.callID || finalCall.Namespace() != state.namespace || finalCall.Name() != state.name || finalCall.Input() != string(state.customInput) || len(state.contents) != 0 || len(state.arguments) != 0 {
			return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
		}
		var call canonical.CustomToolCall
		var err error
		if state.namespace == "" {
			call, err = canonical.NewCustomToolCall(state.id, state.callID, state.name, string(state.customInput))
		} else {
			call, err = canonical.NewNamespacedCustomToolCall(state.id, state.callID, state.namespace, state.name, string(state.customInput))
		}
		if err != nil {
			return canonical.Item{}, 0, err
		}
		item, err := canonical.NewCustomToolCallItem(call)
		if err != nil || !itemEqual(item, final) {
			return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
		}
		return item, 0, nil
	case canonical.ItemType("hosted_search_call"):
		finalCall, ok := final.HostedSearchCall()
		if !ok || !state.searchStatus.Terminal() || finalCall.Status() != state.searchStatus || !hostedSearchActionEqual(finalCall.Action(), state.searchAction) || len(state.contents) != 0 || len(state.arguments) != 0 || len(state.customInput) != 0 {
			return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
		}
		call, err := canonical.NewHostedSearchCall(state.id, state.searchStatus, state.searchAction)
		if err != nil {
			return canonical.Item{}, 0, err
		}
		item, err := canonical.NewHostedSearchCallItem(call)
		if err != nil || !itemEqual(item, final) {
			return canonical.Item{}, 0, eventStateError(CodeCompletionMismatch, "event.item_completed")
		}
		return item, 0, nil
	default:
		return canonical.Item{}, 0, eventStateError(CodeInvalidItemState, "event.item_completed")
	}
}

func completedContent(state *itemState) ([]canonical.ContentBlock, error) {
	if state.nextContent == 0 || len(state.contents) != state.nextContent {
		return nil, eventStateError(CodeInvalidContentState, "event.item_completed")
	}
	content := make([]canonical.ContentBlock, state.nextContent)
	for index := 0; index < state.nextContent; index++ {
		value, ok := state.contents[index]
		if !ok || !value.completed || !value.finalContent.Valid() {
			return nil, eventStateError(CodeInvalidContentState, "event.item_completed")
		}
		content[index] = value.finalContent
	}
	return content, nil
}

func eventStateError(code ErrorCode, path string) error {
	return &Error{Code: code, Path: path, Err: errInvalidTransition}
}
