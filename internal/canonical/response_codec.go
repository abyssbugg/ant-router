package canonical

import (
	"crypto/sha256"
	"encoding/json"
)

// EncodeResponse encodes a validated completed canonical response.
func EncodeResponse(response Response) ([]byte, error) {
	return encodeResponse(response, true)
}

// EncodePortableResponse encodes a response without provider-affine continuation.
func EncodePortableResponse(response Response) ([]byte, error) {
	return encodeResponse(response, false)
}

func encodeResponse(response Response, includeContinuation bool) ([]byte, error) {
	if !response.Valid() {
		return nil, codecError(CodeInvalidInvariant, "response")
	}
	items := make([]json.RawMessage, 0, len(response.Items()))
	for _, item := range response.Items() {
		encoded, err := encodeItem(item)
		if err != nil {
			return nil, err
		}
		items = append(items, encoded)
	}
	dto := responseDTO{Version: ProtocolVersion, ID: response.ID().String(), Model: response.Model(), Items: items, FinishReason: string(response.FinishReason()), Usage: encodeUsage(response.Usage())}
	if includeContinuation {
		if continuation, ok := response.Continuation(); ok {
			dto.Continuation = encodeContinuation(&continuation)
		}
	}
	return marshalCodec(dto, MaxRequestBytes)
}

// FullResponseDigest hashes the full deterministic response encoding.
func FullResponseDigest(response Response) ([sha256.Size]byte, error) {
	encoded, err := EncodeResponse(response)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// PortableResponseDigest hashes the deterministic response encoding without continuation.
func PortableResponseDigest(response Response) ([sha256.Size]byte, error) {
	encoded, err := EncodePortableResponse(response)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// DecodeResponse strictly decodes one completed canonical response.
func DecodeResponse(data []byte) (Response, error) {
	prepared, err := prepareCodecInput(data, MaxRequestBytes)
	if err != nil {
		return Response{}, err
	}
	var dto responseDTO
	if err := strictDecode(prepared, &dto); err != nil {
		return Response{}, err
	}
	if !validProtocolVersion(dto.Version) {
		return Response{}, codecError(CodeInvalidVersion, "version")
	}
	id, err := ParseResponseID(dto.ID)
	if err != nil {
		return Response{}, err
	}
	items := make([]Item, 0, len(dto.Items))
	for _, raw := range dto.Items {
		item, err := decodeItem(raw, dto.Version)
		if err != nil {
			return Response{}, err
		}
		items = append(items, item)
	}
	usage, err := decodeUsage(dto.Usage)
	if err != nil {
		return Response{}, err
	}
	continuation, err := decodeContinuation(dto.Continuation)
	if err != nil {
		return Response{}, err
	}
	return NewResponse(ResponseParams{ID: id, Model: dto.Model, Items: items, FinishReason: FinishReason(dto.FinishReason), Usage: usage, Continuation: continuation})
}
