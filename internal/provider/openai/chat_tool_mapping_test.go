package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

// TestChatWireMapsCustomToolCallsBackToCanonical proves the provider threads
// the request-side tool name map into the response accumulator: a custom tool
// is declared upstream as a plain function, and the upstream function call is
// reconstructed as a canonical custom_tool_call with its raw input.
func TestChatWireMapsCustomToolCallsBackToCanonical(t *testing.T) {
	events := []string{
		`{"id":"c1","model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"up_1","function":{"name":"apply_patch","arguments":"{\"input\":"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"*** Begin Patch\"}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
		`{"id":"c1","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
	}
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requestBody = string(body)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, chatWireSSE(events))
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

	base := chatWireRequest(t)
	tool, err := canonical.NewCustomTool("apply_patch", "apply a patch")
	if err != nil {
		t.Fatal(err)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{
		ID:    base.ID(),
		Model: base.Model(),
		Input: base.Input(),
		Tools: []canonical.ToolDefinition{tool},
	})
	if err != nil {
		t.Fatal(err)
	}

	source, err := provider.Execute(context.Background(), "deepseek-chat", request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()

	if !strings.Contains(requestBody, `"type":"function"`) || !strings.Contains(requestBody, `"name":"apply_patch"`) {
		t.Fatalf("custom tool not declared as upstream function: %s", requestBody)
	}

	machine, err := streaming.NewMachine(streaming.DefaultMachineOptions())
	if err != nil {
		t.Fatal(err)
	}
	var custom canonical.CustomToolCall
	sawCustom, sawInputCompleted := false, false
	for {
		event, recvErr := source.Recv(context.Background())
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatal(recvErr)
		}
		if _, err := machine.Accept(event); err != nil {
			t.Fatalf("machine rejected %q: %v", event.Type(), err)
		}
		if _, ok := event.CustomInputCompleted(); ok {
			sawInputCompleted = true
		}
		if payload, ok := event.ItemCompleted(); ok {
			if call, ok := payload.Item().CustomToolCall(); ok {
				custom, sawCustom = call, true
			}
		}
	}
	if !sawCustom {
		t.Fatal("no canonical custom_tool_call item was produced")
	}
	if !sawInputCompleted {
		t.Fatal("custom_input_completed event was not emitted")
	}
	if custom.Name() != "apply_patch" || custom.Input() != "*** Begin Patch" {
		t.Fatalf("custom call = %q %q", custom.Name(), custom.Input())
	}
}
