package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/credential"
)

type anthropicWireRequest struct {
	APIKey  string
	Version string
	Path    string
	Method  string
	Body    map[string]any
}

func serveAnthropicTextStream(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/event-stream")
	events := []string{
		`{"type":"message_start","message":{"id":"msg_upstream","type":"message","role":"assistant","content":[],"model":"claude-upstream","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":4,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":4,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		var envelope struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(event), &envelope)
		_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", envelope.Type, event)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func TestAnthropicProviderLocalContractTextAndCredentialRotation(t *testing.T) {
	var mu sync.Mutex
	requests := make([]anthropicWireRequest, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, anthropicWireRequest{APIKey: request.Header.Get("X-Api-Key"), Version: request.Header.Get("Anthropic-Version"), Path: request.URL.Path, Method: request.Method, Body: body})
		mu.Unlock()
		serveAnthropicTextStream(writer)
	}))
	defer server.Close()

	creds := &providerCredentialSource{value: "first-private-key"}
	ref, err := credential.NewRef(credential.SourceEnv, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	factory, err := newClientFactory(server.URL, newSecureHTTPClient())
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(creds, ref, anthropicProviderLimits(), &sdkStreamStarter{factory: factory}, &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"first-private-key", "second-private-key"} {
		creds.value = key
		requestContext, cancelRequest := context.WithTimeout(context.Background(), 10*time.Second)
		source, err := provider.Execute(requestContext, "claude-upstream", anthropicTextRequest(t))
		if err != nil {
			cancelRequest()
			t.Fatal(err)
		}
		response, err := drainAnthropicProvider(requestContext, source)
		cancelRequest()
		if err != nil {
			t.Fatal(err)
		}
		if response.Model() != "anthropic/public" || len(response.Items()) != 1 {
			t.Fatalf("response model/items = %q/%d", response.Model(), len(response.Items()))
		}
		message, ok := response.Items()[0].Message()
		if !ok || len(message.Content()) != 1 || message.Content()[0].Type() != "text" {
			t.Fatalf("response item = %#v", response.Items()[0])
		}
		text, ok := message.Content()[0].Text()
		if !ok || text.Text() != "hello" {
			t.Fatalf("response text = %#v", message.Content()[0])
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
	for index, request := range requests {
		wantKey := []string{"first-private-key", "second-private-key"}[index]
		if request.Method != http.MethodPost || request.Path != "/v1/messages" || request.APIKey != wantKey || request.Version != "2023-06-01" {
			t.Fatalf("request %d metadata = %#v", index, request)
		}
		if request.Body["model"] != "claude-upstream" || request.Body["stream"] != true || request.Body["max_tokens"] != json.Number("1024") {
			t.Fatalf("request %d body = %#v", index, request.Body)
		}
		messages, ok := request.Body["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("request %d messages = %#v", index, request.Body["messages"])
		}
	}
}
