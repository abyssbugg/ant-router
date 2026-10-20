package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

func chatRequestWithTools(t *testing.T, tools []canonical.ToolDefinition) canonical.Request {
	t.Helper()
	base := chatWireRequest(t)
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    base.ID(),
		Model: base.Model(),
		Input: base.Input(),
		Tools: tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func chatTestProvider(t *testing.T, url string) *Provider {
	t.Helper()
	provider, err := NewProviderForIDWithEndpoint("deepseek", credential.NewEnvSource(func(string) (string, bool) { return "k", true }), mustChatCredentialRef(t), Endpoint{BaseURL: url, Chat: true})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

// A request the chat wire cannot encode (flattened namespace name over the
// 64-byte chat limit) is a client error, not an upstream failure, and must be
// rejected before any network call.
func TestChatWireRejectsUnencodableToolsAsInvalidRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()

	schema, err := canonicaljson.Parse([]byte(`{"type":"object"}`), canonicaljson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	member, err := canonical.NewToolDefinition(strings.Repeat("m", 40), "member", schema)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := canonical.NewToolNamespace(strings.Repeat("n", 40), "namespace", []canonical.ToolDefinition{member})
	if err != nil {
		t.Fatal(err)
	}
	_, err = chatTestProvider(t, server.URL).Execute(context.Background(), "deepseek-chat", chatRequestWithTools(t, []canonical.ToolDefinition{namespace}))
	if err == nil {
		t.Fatal("Execute() accepted an unencodable tool name")
	}
	if code := execution.CodeOf(err); code != execution.CodeInvalidRequest {
		t.Fatalf("CodeOf(err) = %q, want %q (err=%v)", code, execution.CodeInvalidRequest, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream called %d times for a locally invalid request", calls.Load())
	}
}

// A custom tool input larger than the JSON-arguments bound arrives as one SSE
// data line and must stream through the provider intact.
func TestChatWireStreamsLargeCustomInputInOneSSELine(t *testing.T) {
	inputs := map[string]string{
		"plain":   strings.Repeat("a", canonical.MaxCustomToolInputBytes-(128<<10)),
		"escaped": strings.Repeat(`"`, 600<<10),
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			arguments, err := json.Marshal(map[string]string{"input": input})
			if err != nil {
				t.Fatal(err)
			}
			if len(arguments) <= canonical.MaxJSONBytes {
				t.Fatal("fixture does not exceed MaxJSONBytes")
			}
			chunk, err := json.Marshal(map[string]any{"id": "c1", "model": "deepseek-chat", "choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "id": "up_1", "function": map[string]any{"name": "apply_patch", "arguments": string(arguments)},
				}}},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			events := []string{
				`{"id":"c1","model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
				string(chunk),
				`{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, chatWireSSE(events))
			}))
			defer server.Close()

			tool, err := canonical.NewCustomTool("apply_patch", "apply a patch")
			if err != nil {
				t.Fatal(err)
			}
			source, err := chatTestProvider(t, server.URL).Execute(context.Background(), "deepseek-chat", chatRequestWithTools(t, []canonical.ToolDefinition{tool}))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = source.Close() }()
			machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
			if err != nil {
				t.Fatal(err)
			}
			var got string
			completed := false
			for {
				event, recvErr := source.Recv(context.Background())
				if recvErr == io.EOF {
					break
				}
				if recvErr != nil {
					t.Fatalf("Recv() failed: %v", recvErr)
				}
				transition, err := machine.Accept(event)
				if err != nil {
					t.Fatalf("machine rejected %q: %v", event.Type(), err)
				}
				if transition.TerminalKind() == streaming.TerminalCompleted {
					completed = true
				}
				if payload, ok := event.ItemCompleted(); ok {
					if call, ok := payload.Item().CustomToolCall(); ok {
						got = call.Input()
					}
				}
			}
			if !completed {
				t.Fatal("stream did not complete")
			}
			if got != input {
				t.Fatalf("custom input length %d, want %d", len(got), len(input))
			}
		})
	}
}

// An invalid upstream model comes from provider or route configuration, not
// from the client, so it must not be reported as a non-retryable invalid
// request that stops failover.
func TestChatWireReportsInvalidUpstreamModelAsInternal(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, model := range []string{"", " deepseek-chat", "deepseek-chat "} {
		_, err := chatTestProvider(t, server.URL).executeChat(context.Background(), model, chatWireRequest(t))
		if code := execution.CodeOf(err); code != execution.CodeInternal {
			t.Fatalf("executeChat(%q) CodeOf = %q, want %q (err=%v)", model, code, execution.CodeInternal, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream called %d times for a misconfigured model", calls.Load())
	}
}
