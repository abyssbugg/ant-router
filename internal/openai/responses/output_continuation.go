package responses

import (
	"errors"

	"github.com/abyssbugg/ant-router/internal/canonical"
	openaicontinuation "github.com/abyssbugg/ant-router/internal/openai/continuation"
)

type reasoningOutputEntries map[canonical.ItemID]openaicontinuation.ContinuationEntry

func responseReasoningEntries(response canonical.Response) (reasoningOutputEntries, error) {
	value, ok := response.Continuation()
	if !ok {
		return nil, nil
	}
	anchors := make([]canonical.ItemID, 0, len(response.Items()))
	for _, item := range response.Items() {
		if item.Type() == canonical.ItemType("reasoning_summary") {
			anchors = append(anchors, item.ID())
		}
	}
	entries, err := openaicontinuation.DecodeContinuation(value, value.Provider(), value.Model(), anchors)
	if err != nil {
		return nil, errors.New("response continuation is invalid")
	}
	return indexReasoningEntries(entries), nil
}

func continuationReasoningEntries(value canonical.Continuation) (reasoningOutputEntries, error) {
	entries, err := openaicontinuation.DecodeContinuation(value, value.Provider(), value.Model(), nil)
	if err != nil {
		return nil, errors.New("response continuation is invalid")
	}
	return indexReasoningEntries(entries), nil
}

func indexReasoningEntries(entries []openaicontinuation.ContinuationEntry) reasoningOutputEntries {
	indexed := make(reasoningOutputEntries, len(entries))
	for _, entry := range entries {
		indexed[entry.CanonicalItemID()] = entry
	}
	return indexed
}

func reasoningWireIdentity(itemID canonical.ItemID, entries reasoningOutputEntries) (string, string) {
	if entry, ok := entries[itemID]; ok {
		return entry.UpstreamItemID(), entry.EncryptedContent()
	}
	return wireItemID("rs", itemID), ""
}
