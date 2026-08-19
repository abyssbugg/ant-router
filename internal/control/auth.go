package control

import (
	"net/http"
	"strings"
)

const (
	BearerTokenLength      = 43
	MaxAuthorizationLength = 64
)

type TokenVerifier interface {
	Verify(string) bool
}

func Authenticate(verifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			values := request.Header.Values("Authorization")
			if len(values) != 1 || len(values[0]) > MaxAuthorizationLength || !strings.HasPrefix(values[0], "Bearer ") {
				writeUnauthorized(writer, request)
				return
			}
			token := strings.TrimPrefix(values[0], "Bearer ")
			if len(token) != BearerTokenLength || strings.ContainsAny(token, " \t\r\n,") || verifier == nil || !verifier.Verify(token) {
				writeUnauthorized(writer, request)
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func writeUnauthorized(writer http.ResponseWriter, request *http.Request) {
	WriteError(writer, http.StatusUnauthorized, CodeUnauthorized, RequestIDFromContext(request.Context()))
}
