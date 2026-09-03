package responses

import (
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
)

type continuationOutputEntries map[canonical.ItemID]openaicontinuation.ContinuationEntry

func responseContinuationEntries(response canonical.Response) (continuationOutputEntries, error) {
	value, ok := response.Continuation()
	if !ok {
		return nil, nil
	}
	anchors := make([]canonical.ItemID, 0, len(response.Items()))
	for _, item := range response.Items() {
		if item.Type() == canonical.ItemType("reasoning_summary") || item.Type() == canonical.ItemType("tool_call") {
			anchors = append(anchors, item.ID())
		}
	}
	entries, err := openaicontinuation.DecodeContinuation(value, value.Provider(), value.Model(), anchors)
	if err != nil {
		return nil, errors.New("response continuation is invalid")
	}
	return indexContinuationEntries(entries), nil
}

func continuationOutputIndex(value canonical.Continuation) (continuationOutputEntries, error) {
	entries, err := openaicontinuation.DecodeContinuation(value, value.Provider(), value.Model(), nil)
	if err != nil {
		return nil, errors.New("response continuation is invalid")
	}
	return indexContinuationEntries(entries), nil
}

func indexContinuationEntries(entries []openaicontinuation.ContinuationEntry) continuationOutputEntries {
	indexed := make(continuationOutputEntries, len(entries))
	for _, entry := range entries {
		indexed[entry.CanonicalItemID()] = entry
	}
	return indexed
}

func reasoningWireIdentity(itemID canonical.ItemID, entries continuationOutputEntries) (string, string) {
	if entry, ok := entries[itemID]; ok && entry.IsReasoning() {
		return entry.UpstreamItemID(), entry.EncryptedContent()
	}
	return wireItemID("rs", itemID), ""
}

func toolCallWireIdentity(itemID canonical.ItemID, callID canonical.ToolCallID, entries continuationOutputEntries) (string, string) {
	if entry, ok := entries[itemID]; ok && entry.IsFunctionCall() && entry.CanonicalCallID() == callID {
		return entry.UpstreamItemID(), entry.UpstreamCallID()
	}
	return wireItemID("fc", itemID), wireCallID(callID)
}
