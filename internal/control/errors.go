package control

import (
	"encoding/json"
	"net/http"
)

type ErrorCode string

const (
	CodeUnauthorized         ErrorCode = "unauthorized"
	CodeInvalidRequest       ErrorCode = "invalid_request"
	CodeNotFound             ErrorCode = "not_found"
	CodeMethodNotAllowed     ErrorCode = "method_not_allowed"
	CodeBodyTooLarge         ErrorCode = "body_too_large"
	CodeUnsupportedMediaType ErrorCode = "unsupported_media_type"
	CodeRequestTimeout       ErrorCode = "request_timeout"
	CodeServiceUnavailable   ErrorCode = "service_unavailable"
	CodeInternalError        ErrorCode = "internal_error"
)

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	RequestID string    `json:"request_id"`
}

func safeMessage(code ErrorCode) string {
	switch code {
	case CodeUnauthorized:
		return "authorization failed"
	case CodeInvalidRequest:
		return "invalid request"
	case CodeNotFound:
		return "resource not found"
	case CodeMethodNotAllowed:
		return "method not allowed"
	case CodeBodyTooLarge:
		return "request body too large"
	case CodeUnsupportedMediaType:
		return "unsupported media type"
	case CodeRequestTimeout:
		return "request timed out"
	case CodeServiceUnavailable:
		return "service unavailable"
	default:
		return "internal error"
	}
}

func setJSONHeaders(header http.Header) {
	header.Set("Content-Type", JSONContentType)
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
}

func WriteJSON(writer http.ResponseWriter, status int, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	setJSONHeaders(writer.Header())
	writer.WriteHeader(status)
	_, err = writer.Write(data)
	return err
}

func WriteError(writer http.ResponseWriter, status int, code ErrorCode, requestID string) error {
	if requestID != "" {
		writer.Header().Set(RequestIDHeader, requestID)
	}
	if code == CodeUnauthorized {
		writer.Header().Set("WWW-Authenticate", "Bearer")
	}
	return WriteJSON(writer, status, ErrorResponse{Error: ErrorDetail{Code: code, Message: safeMessage(code), RequestID: requestID}})
}
