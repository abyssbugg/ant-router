package dataplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/provider/mock"
)

func TestHandlerRequiresInferenceTokenAndLoopbackHost(t *testing.T) {
	handler, token := testHandler(t)
	for _, tc := range []struct {
		name, host, auth string
		status           int
	}{
		{"missing token", "127.0.0.1:8787", "", http.StatusUnauthorized},
		{"control-like token", "127.0.0.1:8787", "Bearer wrong", http.StatusUnauthorized},
		{"foreign host", "example.com", "Bearer " + token.String(), http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			req.Host, req.Header = tc.host, make(http.Header)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), token.String()) || strings.Contains(recorder.Body.String(), "wrong") {
				t.Fatal("error leaked credential")
			}
		})
	}
}

func TestHandlerContainsPanicsWithoutLeakingValues(t *testing.T) {
	_, token := testHandler(t)
	provider, _ := mock.NewRandom(time.Millisecond)
	handler, err := NewHandler(HandlerOptions{
		TokenSource: TokenSourceFunc(func(context.Context) (inferencetoken.Token, error) { panic("ultra-private-panic-value") }),
		Executor:    provider, Catalog: provider, ListenAddress: "127.0.0.1:8787", MaxConcurrent: 1,
		RequestTimeout: time.Second, Clock: func() time.Time { return time.Unix(1, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := serve(t, handler, token, http.MethodGet, "/v1/models", "")
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "ultra-private-panic-value") {
		t.Fatalf("panic response = %d/%s", recorder.Code, recorder.Body.String())
	}
}

func TestModelsReturnsExactMockCatalog(t *testing.T) {
	handler, token := testHandler(t)
	recorder := serve(t, handler, token, http.MethodGet, "/v1/models", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []string{"mock/codex", "mock/disconnect", "mock/json", "mock/reasoning", "mock/slow", "mock/text", "mock/tool", "mock/vision"}
	if body.Object != "list" || len(body.Data) != len(want) {
		t.Fatalf("models = %#v", body)
	}
	for index := range want {
		if body.Data[index].ID != want[index] || body.Data[index].Object != "model" || body.Data[index].OwnedBy != "mock" || body.Data[index].Created != 1_700_000_000 {
			t.Fatalf("model[%d] = %#v", index, body.Data[index])
		}
	}
}

func TestResponsesNonStreamingUsesCanonicalPipeline(t *testing.T) {
	handler, token := testHandler(t)
	recorder := serve(t, handler, token, http.MethodPost, "/v1/responses", `{"model":"mock/text","input":"hello"}`)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status/headers = %d/%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["object"] != "response" || body["status"] != "completed" || body["model"] != "mock/text" {
		t.Fatalf("response = %#v", body)
	}
}

func TestResponsesStreamingUsesCanonicalPump(t *testing.T) {
	handler, token := testHandler(t)
	recorder := serve(t, handler, token, http.MethodPost, "/v1/responses", `{"model":"mock/text","input":"hello","stream":true}`)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status/headers = %d/%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "event: response.created\n") || !strings.Contains(body, "event: response.completed\n") || !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("stream = %s", body)
	}
}

func TestResponsesReturnsSafeStrictErrors(t *testing.T) {
	handler, token := testHandler(t)
	secret := "ultra-private-request-value"
	recorder := serve(t, handler, token, http.MethodPost, "/v1/responses", `{"model":"mock/text","input":"hello","unknown":"`+secret+`"}`)
	if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("status/body = %d/%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Error struct{ Type, Code, Param, Message string } `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Type != "invalid_request_error" || body.Error.Code != "unknown_field" || body.Error.Param != "unknown" || body.Error.Message == "" {
		t.Fatalf("error = %#v", body.Error)
	}
}

func TestWriteExecutionErrorReportsInvalidRequestPath(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	writeExecutionError(recorder, execution.NewError(execution.CodeInvalidRequest, "request.continuation", 0, nil))
	var body struct {
		Error struct{ Param string } `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Param != "request.continuation" {
		t.Fatalf("param = %q, want request.continuation", body.Error.Param)
	}
}

func TestWriteExecutionErrorClassifiesByCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code      execution.ErrorCode
		status    int
		errorType string
	}{
		{execution.CodeInvalidRequest, http.StatusBadRequest, "invalid_request_error"},
		{execution.CodeInvalidModel, http.StatusBadRequest, "invalid_request_error"},
		{execution.CodeInternal, http.StatusBadGateway, "server_error"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		writeExecutionError(recorder, execution.NewError(tc.code, "test", 0, nil))
		var body struct {
			Error struct{ Type, Code string } `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != tc.status || body.Error.Type != tc.errorType || body.Error.Code != string(tc.code) {
			t.Errorf("%s: status=%d type=%q code=%q, want %d %q", tc.code, recorder.Code, body.Error.Type, body.Error.Code, tc.status, tc.errorType)
		}
	}
}

func testHandler(t testing.TB) (http.Handler, inferencetoken.Token) {
	t.Helper()
	token := inferencetoken.Token{}
	for index := range token {
		token[index] = 7
	}
	provider, err := mock.NewRandom(time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(HandlerOptions{
		TokenSource: StaticTokenSource(token), Executor: provider, Catalog: provider,
		ListenAddress: "127.0.0.1:8787", MaxConcurrent: 4, RequestTimeout: time.Second,
		Clock: func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, token
}

func serve(t *testing.T, handler http.Handler, token inferencetoken.Token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Authorization", "Bearer "+token.String())
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req.WithContext(context.Background()))
	return recorder
}
