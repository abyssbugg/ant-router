package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

func mustChatCredentialRef(t *testing.T) credential.Ref {
	t.Helper()
	ref, err := credential.NewRef(credential.SourceEnv, "OPENAI_DEEPSEEK_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func chatWireRequest(t *testing.T) canonical.Request {
	t.Helper()
	text, err := canonical.NewTextContent("inspect the payload")
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessage(chatTestItemID(t, 1), canonical.Role("user"), []canonical.ContentBlock{block})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatal(err)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    chatTestRequestID(t, 1),
		Model: "deepseek-chat",
		Input: []canonical.Item{item},
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func chatWireEvents() []string {
	return []string{
		`{"id":"c1","model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"up_1","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
		`{"id":"c1","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
	}
}

func chatWireSSE(events []string) string {
	frames := make([]string, 0, len(events)+1)
	for _, event := range events {
		frames = append(frames, "data: "+event+"\n\n")
	}
	frames = append(frames, "data: [DONE]\n\n")
	return strings.Join(frames, "")
}

func chatTestItemID(t *testing.T, value byte) canonical.ItemID {
	t.Helper()
	id, err := canonical.GenerateItemID(digestReader(value))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func chatTestRequestID(t *testing.T, value byte) canonical.RequestID {
	t.Helper()
	id, err := canonical.GenerateRequestID(digestReader(value))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func digestReader(value byte) io.Reader {
	return strings.NewReader(strings.Repeat(string([]byte{value}), 64))
}

func TestChatWireExecutesAgainstCompatibleEndpoint(t *testing.T) {
	var authHeader, requestPath, requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		requestPath = request.URL.Path
		body, _ := io.ReadAll(request.Body)
		requestBody = string(body)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, chatWireSSE(chatWireEvents()))
	}))
	defer server.Close()

	provider, err := NewProviderForIDWithEndpoint("deepseek", credential.NewEnvSource(func(name string) (string, bool) {
		if name == "OPENAI_DEEPSEEK_KEY" {
			return "secret-test-key", true
		}
		return "", false
	}), mustChatCredentialRef(t), Endpoint{BaseURL: server.URL, Chat: true})
	if err != nil {
		t.Fatal(err)
	}
	request := chatWireRequest(t)
	source, err := provider.Execute(context.Background(), "deepseek-chat", request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()

	if authHeader != "Bearer secret-test-key" {
		t.Fatalf("authorization = %q", authHeader)
	}
	if requestPath != "/chat/completions" {
		t.Fatalf("path = %q", requestPath)
	}
	for _, want := range []string{`"model":"deepseek-chat"`, `"stream":true`, `"include_usage":true`} {
		if !strings.Contains(requestBody, want) {
			t.Fatalf("request body missing %s: %s", want, requestBody)
		}
	}

	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	var terminal canonical.Event
	for {
		event, recvErr := source.Recv(context.Background())
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatal(recvErr)
		}
		transition, err := machine.Accept(event)
		if err != nil {
			t.Fatalf("machine rejected %q: %v", event.Type(), err)
		}
		if transition.TerminalKind() == streaming.TerminalCompleted {
			terminal = event
		}
	}
	completed, ok := terminal.ResponseCompleted()
	if !ok || completed.FinishReason() != canonical.FinishReason("tool_call") {
		t.Fatalf("terminal = %#v", terminal)
	}
	if len(completed.ItemIDs()) != 2 {
		t.Fatalf("items = %d, want 2 (message + tool call)", len(completed.ItemIDs()))
	}
}

func TestChatWireMapsHTTPErrorsWithoutRetry(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		callCount.Add(1)
		writer.Header().Set("Retry-After", "5")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{"error":{"message":"slow down"}}`)
	}))
	defer server.Close()
	provider, err := NewProviderForIDWithEndpoint("deepseek", credential.NewEnvSource(func(string) (string, bool) { return "k", true }), mustChatCredentialRef(t), Endpoint{BaseURL: server.URL, Chat: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Execute(context.Background(), "deepseek-chat", chatWireRequest(t))
	if execution.CodeOf(err) != execution.CodeRateLimited {
		t.Fatalf("error = %v, want rate_limited", err)
	}
	if got := callCount.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want exactly 1 (no retries)", got)
	}
}

func TestChatWireFailsOnTruncatedStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer server.Close()
	provider, err := NewProviderForIDWithEndpoint("deepseek", credential.NewEnvSource(func(string) (string, bool) { return "k", true }), mustChatCredentialRef(t), Endpoint{BaseURL: server.URL, Chat: true})
	if err != nil {
		t.Fatal(err)
	}
	source, err := provider.Execute(context.Background(), "deepseek-chat", chatWireRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	for {
		_, recvErr := source.Recv(context.Background())
		if recvErr == io.EOF {
			t.Fatal("truncated stream reported EOF without a failure")
		}
		if recvErr != nil {
			break
		}
	}
}
