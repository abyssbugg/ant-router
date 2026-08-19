package control

import (
	"net/http"
)

const MaxRequestBody int64 = 4 << 10

func RejectBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := RequestIDFromContext(request.Context())
		if request.ContentLength > MaxRequestBody {
			WriteError(writer, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, requestID)
			return
		}
		if request.ContentLength < 0 || request.ContentLength > 0 || len(request.TransferEncoding) > 0 {
			WriteError(writer, http.StatusBadRequest, CodeInvalidRequest, requestID)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
