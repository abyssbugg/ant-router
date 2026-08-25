package control

import (
	"bytes"
	"io"
	"mime"
	"net/http"
)

func RequestPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/config/validate", "/v1/config/apply":
			if request.Method != http.MethodPost {
				next.ServeHTTP(writer, request)
				return
			}
			if !isJSONContentType(request.Header.Get("Content-Type")) {
				_ = WriteError(writer, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, RequestIDFromContext(request.Context()))
				return
			}
			if !readBoundedRequest(writer, request, MaxConfigRequestBody) {
				return
			}
		case "/v1/config/rollback":
			if request.Method == http.MethodPost && (request.ContentLength != 0 || len(request.TransferEncoding) != 0) {
				if !isJSONContentType(request.Header.Get("Content-Type")) {
					_ = WriteError(writer, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, RequestIDFromContext(request.Context()))
					return
				}
				if !readBoundedRequest(writer, request, MaxRequestBody) {
					return
				}
			}
		default:
			RejectBodies(next).ServeHTTP(writer, request)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

func readBoundedRequest(writer http.ResponseWriter, request *http.Request, maximum int64) bool {
	if request.ContentLength > maximum {
		_ = WriteError(writer, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, RequestIDFromContext(request.Context()))
		return false
	}
	originalBody := request.Body
	defer func() { _ = originalBody.Close() }()
	data, err := io.ReadAll(io.LimitReader(originalBody, maximum+1))
	if err != nil {
		_ = WriteError(writer, http.StatusBadRequest, CodeInvalidRequest, RequestIDFromContext(request.Context()))
		return false
	}
	if int64(len(data)) > maximum {
		_ = WriteError(writer, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, RequestIDFromContext(request.Context()))
		return false
	}
	request.Body = io.NopCloser(bytes.NewReader(data))
	request.ContentLength = int64(len(data))
	return true
}
