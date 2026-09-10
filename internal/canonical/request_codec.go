package canonical

import (
	"crypto/sha256"
	"encoding/json"
)

// EncodeRequest encodes a validated request with provider continuation state.
func EncodeRequest(request Request) ([]byte, error) {
	return encodeRequest(request, true)
}

// EncodePortableRequest encodes a validated request without provider-affine state.
func EncodePortableRequest(request Request) ([]byte, error) {
	return encodeRequest(request, false)
}

func encodeRequest(request Request, includeContinuation bool) ([]byte, error) {
	if !request.Valid() {
		return nil, codecError(CodeInvalidInvariant, "request")
	}
	input := make([]json.RawMessage, 0, len(request.Input()))
	for _, item := range request.Input() {
		encoded, err := encodeItem(item)
		if err != nil {
			return nil, err
		}
		input = append(input, encoded)
	}
	tools := make([]toolDTO, 0, len(request.Tools()))
	for _, tool := range request.Tools() {
		tools = append(tools, encodeTool(tool))
	}
	dto := requestDTO{
		Version:          ProtocolVersion,
		ID:               request.ID().String(),
		Model:            request.Model(),
		Input:            input,
		Tools:            tools,
		ToolChoice:       encodeToolChoice(request.ToolChoice()),
		Output:           encodeOutput(request.Output()),
		Sampling:         encodeSampling(request.Sampling()),
		ReasoningSummary: string(request.ReasoningSummary()),
	}
	if includeContinuation {
		dto.Continuation = encodeContinuation(request.Continuation())
	}
	return marshalCodec(dto, MaxRequestBytes)
}

// DecodeRequest strictly decodes one canonical request document.
func DecodeRequest(data []byte) (Request, error) {
	prepared, err := prepareCodecInput(data, MaxRequestBytes)
	if err != nil {
		return Request{}, err
	}
	var dto requestDTO
	if err := strictDecode(prepared, &dto); err != nil {
		return Request{}, err
	}
	if !validProtocolVersion(dto.Version) {
		return Request{}, codecError(CodeInvalidVersion, "version")
	}
	id, err := ParseRequestID(dto.ID)
	if err != nil {
		return Request{}, err
	}
	input := make([]Item, 0, len(dto.Input))
	for _, raw := range dto.Input {
		item, err := decodeItem(raw, dto.Version)
		if err != nil {
			return Request{}, err
		}
		input = append(input, item)
	}
	tools := make([]ToolDefinition, 0, len(dto.Tools))
	for _, raw := range dto.Tools {
		tool, err := decodeTool(raw, dto.Version)
		if err != nil {
			return Request{}, err
		}
		tools = append(tools, tool)
	}
	choice, err := decodeToolChoice(dto.ToolChoice)
	if err != nil {
		return Request{}, err
	}
	output, err := decodeOutput(dto.Output)
	if err != nil {
		return Request{}, err
	}
	sampling, err := decodeSampling(dto.Sampling)
	if err != nil {
		return Request{}, err
	}
	continuation, err := decodeContinuation(dto.Continuation)
	if err != nil {
		return Request{}, err
	}
	return NewRequest(RequestParams{ID: id, Model: dto.Model, Input: input, Tools: tools, ToolChoice: choice, Output: output, Sampling: sampling, Continuation: continuation, ReasoningSummary: ReasoningSummaryMode(dto.ReasoningSummary)})
}

// FullDigest hashes the full deterministic request encoding.
func FullDigest(request Request) ([sha256.Size]byte, error) {
	encoded, err := EncodeRequest(request)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// PortableDigest hashes deterministic request encoding without continuation state.
func PortableDigest(request Request) ([sha256.Size]byte, error) {
	encoded, err := EncodePortableRequest(request)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
