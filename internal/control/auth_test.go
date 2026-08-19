package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type verifierFunc func(string) bool

func (f verifierFunc) Verify(value string) bool { return f(value) }

func TestAuthenticate(t *testing.T) {
	validToken := strings.Repeat("a", 43)
	called := 0
	verifier := verifierFunc(func(value string) bool {
		called++
		return value == validToken
	})
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	tests := []struct {
		name    string
		headers []string
		want    int
	}{
		{name: "missing", want: http.StatusUnauthorized},
		{name: "wrong scheme", headers: []string{"Basic abc"}, want: http.StatusUnauthorized},
		{name: "extra whitespace", headers: []string{"Bearer  " + validToken}, want: http.StatusUnauthorized},
		{name: "multiple", headers: []string{"Bearer " + validToken, "Bearer " + validToken}, want: http.StatusUnauthorized},
		{name: "oversized", headers: []string{"Bearer " + strings.Repeat("a", MaxAuthorizationLength)}, want: http.StatusUnauthorized},
		{name: "wrong token", headers: []string{"Bearer " + strings.Repeat("b", 43)}, want: http.StatusUnauthorized},
		{name: "valid", headers: []string{"Bearer " + validToken}, want: http.StatusNoContent},
		{name: "lowercase scheme", headers: []string{"bearer " + validToken}, want: http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
			request = request.WithContext(WithRequestID(request.Context(), "request-123"))
			for _, value := range test.headers {
				request.Header.Add("Authorization", value)
			}
			recorder := httptest.NewRecorder()
			Authenticate(verifier)(next).ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Errorf("status = %d, want %d", recorder.Code, test.want)
			}
			if test.want == http.StatusUnauthorized {
				if recorder.Header().Get("WWW-Authenticate") != "Bearer" || !strings.Contains(recorder.Body.String(), "authorization failed") {
					t.Errorf("unauthorized response = headers %#v body %q", recorder.Header(), recorder.Body.String())
				}
				if strings.Contains(recorder.Body.String(), validToken) {
					t.Error("response exposed token")
				}
			}
		})
	}
	if called != 3 {
		t.Errorf("verifier calls = %d, want 3 for well-formed wrong and valid tokens", called)
	}
}
