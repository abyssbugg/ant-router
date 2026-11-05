package dataplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatCompletionsNonStreamRoundTrip(t *testing.T) {
	handler, token := testHandler(t)
	recorder := serve(t, handler, token, http.MethodPost, "/v1/chat/completions",
		`{"model":"mock/text","messages":[{"role":"user","content":"hello"}]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	body := recorder.Body.String()
	for _, want := range []string{`"object":"chat.completion"`, `"role":"assistant"`, `"finish_reason":"stop"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("completion missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "response.created") {
		t.Fatal("chat endpoint leaked responses-wire frames")
	}
}

func TestChatCompletionsStreamRoundTrip(t *testing.T) {
	handler, token := testHandler(t)
	recorder := serve(t, handler, token, http.MethodPost, "/v1/chat/completions",
		`{"model":"mock/text","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q", got)
	}
	body := recorder.Body.String()
	for _, want := range []string{`"object":"chat.completion.chunk"`, `"role":"assistant"`, `data: [DONE]`, `"finish_reason":"stop"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sse missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "response.output_text.delta") {
		t.Fatal("chat stream leaked responses-wire frames")
	}
}

func TestChatCompletionsRejectsGetAndBadContentType(t *testing.T) {
	handler, token := testHandler(t)
	recorder := serve(t, handler, token, http.MethodGet, "/v1/chat/completions", "")
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d", recorder.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"mock/text","messages":[{"role":"user","content":"x"}]}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Authorization", "Bearer "+token.String())
	req.Header.Set("Content-Type", "text/plain")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type status = %d", recorder.Code)
	}
}

func TestChatCompletionsRejectsInvalidRequests(t *testing.T) {
	handler, token := testHandler(t)
	for _, tc := range []struct {
		name, body string
	}{
		{"no messages", `{"model":"mock/text","messages":[]}`},
		{"empty model", `{"model":"","messages":[{"role":"user","content":"x"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := serve(t, handler, token, http.MethodPost, "/v1/chat/completions", tc.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestChatCompletionsRequiresAuthAndHost(t *testing.T) {
	handler, token := testHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"mock/text","messages":[{"role":"user","content":"x"}]}`))
	req.Host = "evil.example"
	req.Header.Set("Authorization", "Bearer "+token.String())
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("foreign host status = %d", recorder.Code)
	}
	_ = token
}
