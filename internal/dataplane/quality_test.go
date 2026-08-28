package dataplane

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisconnectFailsBeforeAnySSECommit(t *testing.T) {
	handler, token := testHandler(t)
	for _, stream := range []bool{false, true} {
		body := `{"model":"mock/disconnect","input":"hello"}`
		if stream {
			body = `{"model":"mock/disconnect","input":"hello","stream":true}`
		}
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/v1/responses", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token.String())
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("stream %t status = %d", stream, response.Code)
		}
		if got := response.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("stream %t content type = %q", stream, got)
		}
		if strings.Contains(response.Body.String(), "response.created") || strings.Contains(response.Body.String(), token.String()) {
			t.Fatalf("stream %t leaked precommit state", stream)
		}
	}
}

func FuzzInferenceBoundary(f *testing.F) {
	handler, token := testHandler(f)
	valid := []byte(`{"model":"mock/text","input":"hello"}`)
	f.Add("Bearer "+token.String(), "127.0.0.1:8787", "application/json", valid)
	f.Add("Bearer invalid", "localhost:8787", "text/plain", []byte(`{"secret":"do-not-leak"}`))
	f.Fuzz(func(t *testing.T, authorization, host, contentType string, body []byte) {
		if len(body) > 2<<20 {
			t.Skip()
		}
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/v1/responses", bytes.NewReader(body))
		request.Host = host
		request.Header.Set("Authorization", authorization)
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code < 200 || response.Code > 599 {
			t.Fatalf("invalid status %d", response.Code)
		}
		if strings.Contains(response.Body.String(), token.String()) || strings.Contains(response.Body.String(), "do-not-leak") {
			t.Fatal("response leaked protected input")
		}
	})
}
