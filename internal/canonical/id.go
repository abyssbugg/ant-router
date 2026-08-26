package canonical

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

const (
	// IDRandomBytes is the random payload size of every generated canonical ID.
	IDRandomBytes  = 16
	idPayloadBytes = 22
)

var idEncoding = base64.RawURLEncoding.Strict()

// RequestID identifies one canonical request.
type RequestID string

// ResponseID identifies one canonical response.
type ResponseID string

// ItemID identifies one canonical input or output item.
type ItemID string

// ToolCallID identifies one canonical tool invocation across call and result items.
type ToolCallID string

// EventID identifies one canonical stream event when an event ID is required.
type EventID string

// GenerateRequestID generates a request ID from reader or crypto/rand when nil.
func GenerateRequestID(reader io.Reader) (RequestID, error) {
	value, err := generateID("req_", reader)
	return RequestID(value), err
}

// GenerateResponseID generates a response ID from reader or crypto/rand when nil.
func GenerateResponseID(reader io.Reader) (ResponseID, error) {
	value, err := generateID("resp_", reader)
	return ResponseID(value), err
}

// GenerateItemID generates an item ID from reader or crypto/rand when nil.
func GenerateItemID(reader io.Reader) (ItemID, error) {
	value, err := generateID("item_", reader)
	return ItemID(value), err
}

// GenerateToolCallID generates a tool-call ID from reader or crypto/rand when nil.
func GenerateToolCallID(reader io.Reader) (ToolCallID, error) {
	value, err := generateID("call_", reader)
	return ToolCallID(value), err
}

// GenerateEventID generates an event ID from reader or crypto/rand when nil.
func GenerateEventID(reader io.Reader) (EventID, error) {
	value, err := generateID("evt_", reader)
	return EventID(value), err
}

// ParseRequestID validates and returns a request ID.
func ParseRequestID(value string) (RequestID, error) {
	if err := validateID("req_", value); err != nil {
		return "", err
	}
	return RequestID(value), nil
}

// ParseResponseID validates and returns a response ID.
func ParseResponseID(value string) (ResponseID, error) {
	if err := validateID("resp_", value); err != nil {
		return "", err
	}
	return ResponseID(value), nil
}

// ParseItemID validates and returns an item ID.
func ParseItemID(value string) (ItemID, error) {
	if err := validateID("item_", value); err != nil {
		return "", err
	}
	return ItemID(value), nil
}

// ParseToolCallID validates and returns a tool-call ID.
func ParseToolCallID(value string) (ToolCallID, error) {
	if err := validateID("call_", value); err != nil {
		return "", err
	}
	return ToolCallID(value), nil
}

// ParseEventID validates and returns an event ID.
func ParseEventID(value string) (EventID, error) {
	if err := validateID("evt_", value); err != nil {
		return "", err
	}
	return EventID(value), nil
}

// String returns the canonical request ID.
func (id RequestID) String() string { return string(id) }

// String returns the canonical response ID.
func (id ResponseID) String() string { return string(id) }

// String returns the canonical item ID.
func (id ItemID) String() string { return string(id) }

// String returns the canonical tool-call ID.
func (id ToolCallID) String() string { return string(id) }

// String returns the canonical event ID.
func (id EventID) String() string { return string(id) }

// Valid reports whether the request ID is canonical.
func (id RequestID) Valid() bool { return validateID("req_", string(id)) == nil }

// Valid reports whether the response ID is canonical.
func (id ResponseID) Valid() bool { return validateID("resp_", string(id)) == nil }

// Valid reports whether the item ID is canonical.
func (id ItemID) Valid() bool { return validateID("item_", string(id)) == nil }

// Valid reports whether the tool-call ID is canonical.
func (id ToolCallID) Valid() bool { return validateID("call_", string(id)) == nil }

// Valid reports whether the event ID is canonical.
func (id EventID) Valid() bool { return validateID("evt_", string(id)) == nil }

func generateID(prefix string, reader io.Reader) (string, error) {
	if reader == nil {
		reader = rand.Reader
	}
	material := make([]byte, IDRandomBytes)
	if _, err := io.ReadFull(reader, material); err != nil {
		return "", &Error{Code: CodeInvalidID, Path: "id", Err: errors.New("ID generation failed")}
	}
	return prefix + idEncoding.EncodeToString(material), nil
}

func validateID(prefix, value string) error {
	if len(value) != len(prefix)+idPayloadBytes || len(value) < len(prefix) || value[:len(prefix)] != prefix {
		return invalidIDError()
	}
	payload := value[len(prefix):]
	decoded, err := idEncoding.DecodeString(payload)
	if err != nil || len(decoded) != IDRandomBytes || idEncoding.EncodeToString(decoded) != payload {
		return invalidIDError()
	}
	return nil
}

func invalidIDError() error {
	return &Error{Code: CodeInvalidID, Path: "id", Err: errors.New("ID is invalid")}
}
