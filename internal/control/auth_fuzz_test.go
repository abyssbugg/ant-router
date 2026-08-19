package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func FuzzAuthorizationHeader(f *testing.F) {
	validToken := strings.Repeat("a", BearerTokenLength)
	f.Add("")
	f.Add("Bearer " + validToken)
	f.Add("Basic value")
	f.Add("Bearer malformed,value")
	f.Fuzz(func(t *testing.T, header string) {
		verifier := verifierFunc(func(value string) bool { return value == validToken })
		handler := Authenticate(verifier)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}))
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/health", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent && recorder.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status %d", recorder.Code)
		}
		if strings.Contains(recorder.Body.String(), validToken) {
			t.Fatal("response exposed valid token")
		}
	})
}
