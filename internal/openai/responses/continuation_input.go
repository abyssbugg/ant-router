package responses

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/abyssbugg/ant-router/internal/canonical"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
)

const maxReplayWireIDBytes = 256

func decodeReasoningReplay(fields map[string]json.RawMessage, ids IDGenerator, path string) (canonical.Item, openaicontinuation.ContinuationEntry, error) {
	if err := rejectFields(fields, path, "type", "id", "summary", "encrypted_content", "status", "content"); err != nil {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, err
	}
	if _, exists := fields["content"]; exists {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeUnsupportedField, path+".content", errors.New("reasoning content is unsupported"))
	}
	upstreamID, err := requiredString(fields, "id", path+".id")
	if err != nil {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, err
	}
	encrypted, err := requiredString(fields, "encrypted_content", path+".encrypted_content")
	if err != nil {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, err
	}
	if err := validateCompletedStatus(fields, path); err != nil {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, err
	}
	rawSummary, exists := fields["summary"]
	if !exists {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeInvalidField, path+".summary", errors.New("summary is required"))
	}
	var values []json.RawMessage
	if json.Unmarshal(rawSummary, &values) != nil || values == nil || len(values) > canonical.MaxContentBlocks {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeInvalidField, path+".summary", errors.New("summary is invalid"))
	}
	summaries := make([]canonical.TextContent, 0, len(values))
	for index, raw := range values {
		itemPath := path + ".summary[" + strconv.Itoa(index) + "]"
		entryFields, err := strictObject(raw, itemPath, "type", "text")
		if err != nil {
			return canonical.Item{}, openaicontinuation.ContinuationEntry{}, err
		}
		kind, err := requiredString(entryFields, "type", itemPath+".type")
		if err != nil || kind != "summary_text" {
			return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeInvalidField, itemPath+".type", errors.New("summary type is invalid"))
		}
		text, err := requiredString(entryFields, "text", itemPath+".text")
		if err != nil {
			return canonical.Item{}, openaicontinuation.ContinuationEntry{}, err
		}
		value, err := canonical.NewTextContent(text)
		if err != nil {
			return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeInvalidField, itemPath+".text", errors.New("summary text is invalid"))
		}
		summaries = append(summaries, value)
	}
	itemID, err := ids.ItemID()
	if err != nil {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeInternal, path+".id", errors.New("ID generation failed"))
	}
	reasoning, err := canonical.NewReasoningSummary(itemID, summaries)
	if err != nil {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeInvalidField, path, errors.New("reasoning item is invalid"))
	}
	item, err := canonical.NewReasoningSummaryItem(reasoning)
	if err != nil {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeInvalidField, path, errors.New("reasoning item is invalid"))
	}
	continuationEntry, err := openaicontinuation.NewContinuationEntry(itemID, upstreamID, encrypted)
	if err != nil {
		return canonical.Item{}, openaicontinuation.ContinuationEntry{}, newError(CodeInvalidField, path, errors.New("reasoning continuation is invalid"))
	}
	return item, continuationEntry, nil
}

func validateReplayMetadata(fields map[string]json.RawMessage, path string) error {
	if rawID, exists := fields["id"]; exists {
		var id string
		if json.Unmarshal(rawID, &id) != nil || id == "" || len(id) > maxReplayWireIDBytes {
			return newError(CodeInvalidField, path+".id", errors.New("item ID is invalid"))
		}
	}
	return validateCompletedStatus(fields, path)
}

func validateCompletedStatus(fields map[string]json.RawMessage, path string) error {
	if rawStatus, exists := fields["status"]; exists {
		var status string
		if json.Unmarshal(rawStatus, &status) != nil || status != "completed" {
			return newError(CodeInvalidField, path+".status", errors.New("item status is invalid"))
		}
	}
	return nil
}
