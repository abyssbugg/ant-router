package continuation

import (
	"bytes"
	"encoding/json"
	"errors"
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

const functionCallEntryKind = "function_call"

type continuationEntry struct {
	kind             string
	canonicalItemID  canonical.ItemID
	canonicalCallID  canonical.ToolCallID
	upstreamItemID   string
	upstreamCallID   string
	encryptedContent string
}

// ContinuationEntry is one opaque OpenAI reasoning replay entry. Its fields are
// intentionally inaccessible outside this package.
type ContinuationEntry struct{ entry continuationEntry }

// NewContinuationEntry validates one canonical/upstream reasoning anchor and
// opaque encrypted payload without exposing the payload through errors.
func NewContinuationEntry(canonicalItemID canonical.ItemID, upstreamItemID, encryptedContent string) (ContinuationEntry, error) {
	envelope, err := newContinuationEnvelope([]continuationEntry{{canonicalItemID: canonicalItemID, upstreamItemID: upstreamItemID, encryptedContent: encryptedContent}})
	if err != nil {
		return ContinuationEntry{}, err
	}
	return ContinuationEntry{entry: envelope.entries[0]}, nil
}

// NewFunctionCallEntry validates one canonical/upstream function-call identity.
func NewFunctionCallEntry(canonicalItemID canonical.ItemID, canonicalCallID canonical.ToolCallID, upstreamItemID, upstreamCallID string) (ContinuationEntry, error) {
	envelope, err := newContinuationEnvelope([]continuationEntry{{kind: functionCallEntryKind, canonicalItemID: canonicalItemID, canonicalCallID: canonicalCallID, upstreamItemID: upstreamItemID, upstreamCallID: upstreamCallID}})
	if err != nil {
		return ContinuationEntry{}, err
	}
	return ContinuationEntry{entry: envelope.entries[0]}, nil
}

// IsReasoning reports whether this entry carries encrypted reasoning identity.
func (e ContinuationEntry) IsReasoning() bool { return e.entry.kind == "" }

// IsFunctionCall reports whether this entry carries function-call identity.
func (e ContinuationEntry) IsFunctionCall() bool { return e.entry.kind == functionCallEntryKind }

// CanonicalItemID returns the canonical item anchor.
func (e ContinuationEntry) CanonicalItemID() canonical.ItemID { return e.entry.canonicalItemID }

// CanonicalCallID returns the canonical call identity for function-call entries.
func (e ContinuationEntry) CanonicalCallID() canonical.ToolCallID { return e.entry.canonicalCallID }

// UpstreamCallID returns the original OpenAI call identity for function-call entries.
func (e ContinuationEntry) UpstreamCallID() string { return e.entry.upstreamCallID }

// UpstreamItemID returns the original OpenAI reasoning item ID.
func (e ContinuationEntry) UpstreamItemID() string { return e.entry.upstreamItemID }

// EncryptedContent returns opaque OpenAI-encrypted reasoning content. Callers
// must use it only in an authorized OpenAI wire payload and must never log it.
func (e ContinuationEntry) EncryptedContent() string { return e.entry.encryptedContent }

// DecodeContinuation strictly decodes and validates an opaque continuation for
// the expected affinity and optional allowed canonical reasoning anchors.
func DecodeContinuation(value canonical.Continuation, providerID, upstreamModel string, allowedAnchors []canonical.ItemID) ([]ContinuationEntry, error) {
	envelope, err := decodeContinuationEnvelope(value, providerID, upstreamModel, allowedAnchors)
	if err != nil {
		return nil, err
	}
	entries := make([]ContinuationEntry, len(envelope.entries))
	for index, entry := range envelope.entries {
		entries[index] = ContinuationEntry{entry: entry}
	}
	return entries, nil
}

// EncodeContinuation creates a deterministic provider/upstream-affine opaque
// continuation from ordered validated entries.
func EncodeContinuation(providerID, upstreamModel string, entries []ContinuationEntry) (canonical.Continuation, error) {
	values := make([]continuationEntry, len(entries))
	for index, entry := range entries {
		values[index] = entry.entry
	}
	return encodeContinuationEnvelope(providerID, upstreamModel, values)
}

type continuationEnvelope struct {
	entries []continuationEntry
}

type continuationEnvelopeDTO struct {
	Entries []continuationEntryDTO `json:"entries"`
	Version int                    `json:"version"`
}

type continuationEntryDTO struct {
	CanonicalCallID  string `json:"canonical_call_id,omitempty"`
	CanonicalItemID  string `json:"canonical_item_id"`
	EncryptedContent string `json:"encrypted_content,omitempty"`
	Kind             string `json:"kind,omitempty"`
	UpstreamCallID   string `json:"upstream_call_id,omitempty"`
	UpstreamItemID   string `json:"upstream_item_id"`
}

func newContinuationEnvelope(entries []continuationEntry) (continuationEnvelope, error) {
	if len(entries) == 0 || len(entries) > canonical.MaxInputItems {
		return continuationEnvelope{}, continuationEnvelopeError()
	}
	seenItems := make(map[canonical.ItemID]struct{}, len(entries))
	seenUpstream := make(map[string]struct{}, len(entries))
	seenCalls := make(map[canonical.ToolCallID]struct{}, len(entries))
	seenUpstreamCalls := make(map[string]struct{}, len(entries))
	copyEntries := make([]continuationEntry, len(entries))
	for index, entry := range entries {
		if !entry.canonicalItemID.Valid() {
			return continuationEnvelope{}, continuationEnvelopeError()
		}
		switch entry.kind {
		case "":
			if entry.canonicalCallID.Valid() || entry.upstreamCallID != "" || !validUpstreamID(entry.upstreamItemID, "rs_") || !validEncryptedContent(entry.encryptedContent) {
				return continuationEnvelope{}, continuationEnvelopeError()
			}
		case functionCallEntryKind:
			if !entry.canonicalCallID.Valid() || entry.encryptedContent != "" || !validUpstreamID(entry.upstreamItemID, "fc_") || !validUpstreamID(entry.upstreamCallID, "call_") {
				return continuationEnvelope{}, continuationEnvelopeError()
			}
		default:
			return continuationEnvelope{}, continuationEnvelopeError()
		}
		if _, exists := seenItems[entry.canonicalItemID]; exists {
			return continuationEnvelope{}, continuationEnvelopeError()
		}
		if _, exists := seenUpstream[entry.upstreamItemID]; exists {
			return continuationEnvelope{}, continuationEnvelopeError()
		}
		if entry.kind == functionCallEntryKind {
			if _, exists := seenCalls[entry.canonicalCallID]; exists {
				return continuationEnvelope{}, continuationEnvelopeError()
			}
			if _, exists := seenUpstreamCalls[entry.upstreamCallID]; exists {
				return continuationEnvelope{}, continuationEnvelopeError()
			}
			seenCalls[entry.canonicalCallID] = struct{}{}
			seenUpstreamCalls[entry.upstreamCallID] = struct{}{}
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
		if entry.kind == functionCallEntryKind {
			dto.Entries[index].Kind = functionCallEntryKind
			dto.Entries[index].CanonicalCallID = entry.canonicalCallID.String()
			dto.Entries[index].UpstreamCallID = entry.upstreamCallID
		}
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

func validUpstreamReasoningID(value string) bool { return validUpstreamID(value, "rs_") }

func validUpstreamID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) > maxUpstreamItemIDBytes {
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
	return errors.New("openai continuation is invalid")
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
		entry := continuationEntry{kind: item.Kind, canonicalItemID: itemID, upstreamItemID: item.UpstreamItemID, encryptedContent: item.EncryptedContent, upstreamCallID: item.UpstreamCallID}
		if item.CanonicalCallID != "" {
			callID, parseErr := canonical.ParseToolCallID(item.CanonicalCallID)
			if parseErr != nil {
				return continuationEnvelope{}, continuationEnvelopeError()
			}
			entry.canonicalCallID = callID
		}
		entries[index] = entry
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
