package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

const (
	continuationEnvelopeVersion = 1
	maxUpstreamItemIDBytes      = 256
)

type continuationEntry struct {
	canonicalItemID  canonical.ItemID
	upstreamItemID   string
	encryptedContent string
}

type continuationEnvelope struct {
	entries []continuationEntry
}

type continuationEnvelopeDTO struct {
	Entries []continuationEntryDTO `json:"entries"`
	Version int                    `json:"version"`
}

type continuationEntryDTO struct {
	CanonicalItemID  string `json:"canonical_item_id"`
	EncryptedContent string `json:"encrypted_content"`
	UpstreamItemID   string `json:"upstream_item_id"`
}

func newContinuationEnvelope(entries []continuationEntry) (continuationEnvelope, error) {
	if len(entries) == 0 || len(entries) > canonical.MaxInputItems {
		return continuationEnvelope{}, continuationEnvelopeError()
	}
	seenItems := make(map[canonical.ItemID]struct{}, len(entries))
	seenUpstream := make(map[string]struct{}, len(entries))
	copyEntries := make([]continuationEntry, len(entries))
	for index, entry := range entries {
		if !entry.canonicalItemID.Valid() || !validUpstreamReasoningID(entry.upstreamItemID) || !validEncryptedContent(entry.encryptedContent) {
			return continuationEnvelope{}, continuationEnvelopeError()
		}
		if _, exists := seenItems[entry.canonicalItemID]; exists {
			return continuationEnvelope{}, continuationEnvelopeError()
		}
		if _, exists := seenUpstream[entry.upstreamItemID]; exists {
			return continuationEnvelope{}, continuationEnvelopeError()
		}
		seenItems[entry.canonicalItemID] = struct{}{}
		seenUpstream[entry.upstreamItemID] = struct{}{}
		copyEntries[index] = entry
	}
	envelope := continuationEnvelope{entries: copyEntries}
	if _, err := envelope.canonicalBytes(); err != nil {
		return continuationEnvelope{}, err
	}
	return envelope, nil
}

func (e continuationEnvelope) entriesCopy() []continuationEntry {
	return append([]continuationEntry(nil), e.entries...)
}

func (e continuationEnvelope) append(entry continuationEntry) (continuationEnvelope, error) {
	entries := append(e.entriesCopy(), entry)
	return newContinuationEnvelope(entries)
}

func (e continuationEnvelope) canonicalBytes() ([]byte, error) {
	dto := continuationEnvelopeDTO{Version: continuationEnvelopeVersion, Entries: make([]continuationEntryDTO, len(e.entries))}
	for index, entry := range e.entries {
		dto.Entries[index] = continuationEntryDTO{CanonicalItemID: entry.canonicalItemID.String(), UpstreamItemID: entry.upstreamItemID, EncryptedContent: entry.encryptedContent}
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return nil, continuationEnvelopeError()
	}
	value, err := canonicaljson.Parse(raw, canonicaljson.Limits{MaximumBytes: canonical.MaxContinuationBytes, MaximumDepth: 8})
	if err != nil {
		return nil, continuationEnvelopeError()
	}
	return value.Bytes(), nil
}

func validUpstreamReasoningID(value string) bool {
	if !strings.HasPrefix(value, "rs_") || len(value) > maxUpstreamItemIDBytes {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validEncryptedContent(value string) bool {
	if value == "" || len(value) > canonical.MaxContinuationBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func continuationEnvelopeError() error {
	return requestMapError("request.continuation")
}

func encodeContinuationEnvelope(providerID, upstreamModel string, entries []continuationEntry) (canonical.Continuation, error) {
	envelope, err := newContinuationEnvelope(entries)
	if err != nil {
		return canonical.Continuation{}, err
	}
	encoded, err := envelope.canonicalBytes()
	if err != nil {
		return canonical.Continuation{}, err
	}
	continuation, err := canonical.NewContinuation(providerID, upstreamModel, encoded)
	if err != nil {
		return canonical.Continuation{}, continuationEnvelopeError()
	}
	return continuation, nil
}

func decodeContinuationEnvelope(continuation canonical.Continuation, providerID, upstreamModel string, allowedAnchors []canonical.ItemID) (continuationEnvelope, error) {
	if !continuation.Valid() || continuation.Provider() != providerID || continuation.Model() != upstreamModel {
		return continuationEnvelope{}, continuationEnvelopeError()
	}
	value, err := canonicaljson.Parse(continuation.Bytes(), canonicaljson.Limits{MaximumBytes: canonical.MaxContinuationBytes, MaximumDepth: 8})
	if err != nil || !value.IsObject() {
		return continuationEnvelope{}, continuationEnvelopeError()
	}
	decoder := json.NewDecoder(bytes.NewReader(value.Bytes()))
	decoder.DisallowUnknownFields()
	var dto continuationEnvelopeDTO
	if err := decoder.Decode(&dto); err != nil {
		return continuationEnvelope{}, continuationEnvelopeError()
	}
	if err := requireEnvelopeEOF(decoder); err != nil || dto.Version != continuationEnvelopeVersion {
		return continuationEnvelope{}, continuationEnvelopeError()
	}
	entries := make([]continuationEntry, len(dto.Entries))
	for index, item := range dto.Entries {
		itemID, err := canonical.ParseItemID(item.CanonicalItemID)
		if err != nil {
			return continuationEnvelope{}, continuationEnvelopeError()
		}
		entries[index] = continuationEntry{canonicalItemID: itemID, upstreamItemID: item.UpstreamItemID, encryptedContent: item.EncryptedContent}
	}
	envelope, err := newContinuationEnvelope(entries)
	if err != nil {
		return continuationEnvelope{}, err
	}
	canonicalBytes, err := envelope.canonicalBytes()
	if err != nil || !bytes.Equal(canonicalBytes, value.Bytes()) {
		return continuationEnvelope{}, continuationEnvelopeError()
	}
	if allowedAnchors != nil {
		allowed := make(map[canonical.ItemID]struct{}, len(allowedAnchors))
		for _, itemID := range allowedAnchors {
			if !itemID.Valid() {
				return continuationEnvelope{}, continuationEnvelopeError()
			}
			allowed[itemID] = struct{}{}
		}
		for _, entry := range envelope.entries {
			if _, exists := allowed[entry.canonicalItemID]; !exists {
				return continuationEnvelope{}, continuationEnvelopeError()
			}
		}
	}
	return envelope, nil
}

func requireEnvelopeEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return continuationEnvelopeError()
	}
	return nil
}
