package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

func serveSDKEvents(writer http.ResponseWriter, events []sdkresponses.ResponseStreamEventUnion) error {
	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("test server does not support flushing")
	}
	for _, event := range events {
		document := event.RawJSON()
		if document == "" {
			return fmt.Errorf("SDK event has no raw JSON")
		}
		if _, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event.Type, document); err != nil {
			return err
		}
		flusher.Flush()
	}
	return nil
}

func newContractProvider(t *testing.T, server *httptest.Server, source credential.Source) *Provider {
	t.Helper()
	factory, err := newClientFactory(server.URL+"/v1", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(source, openAIRef(t), &sdkStreamStarter{factory: factory}, &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestOpenAIProviderLocalContractTextAndCredentialRotation(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
		if err != nil {
			t.Error(err)
			return
		}
		var document map[string]any
		if err := json.Unmarshal(body, &document); err != nil {
			t.Errorf("request JSON: %v", err)
			return
		}
		if document["model"] != "gpt-test" || document["stream"] != true {
			t.Errorf("model/stream = %#v/%#v", document["model"], document["stream"])
		}
		mu.Lock()
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		mu.Unlock()
		if err := serveSDKEvents(writer, textSDKEvents(t)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	key := "first-contract-key"
	source := credential.NewEnvSource(func(string) (string, bool) { return key, true })
	provider := newContractProvider(t, server, source)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for index := 0; index < 2; index++ {
		events, err := provider.Execute(ctx, "gpt-test", textRequest(t))
		if err != nil {
			t.Fatal(err)
		}
		response, err := drainProviderSource(ctx, events)
		if err != nil {
			t.Fatal(err)
		}
		if response.Model() != "openai/public" || len(response.Items()) != 1 {
			t.Fatalf("response = %#v", response)
		}
		key = "second-contract-key"
	}
	mu.Lock()
	defer mu.Unlock()
	if calls.Load() != 2 || len(authorizations) != 2 || authorizations[0] != "Bearer first-contract-key" || authorizations[1] != "Bearer second-contract-key" {
		t.Fatalf("calls/authorization = %d/%#v", calls.Load(), authorizations)
	}
}

func structuredSDKEvents(t *testing.T) []sdkresponses.ResponseStreamEventUnion {
	t.Helper()
	return []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"resp_json","model":"gpt-upstream","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"msg_json","type":"message","role":"assistant","status":"in_progress","content":[]}}`),
		sdkStreamEvent(t, `{"type":"response.content_part.added","sequence_number":2,"output_index":0,"content_index":0,"item_id":"msg_json","part":{"type":"output_text","text":"","annotations":[]}}`),
		sdkStreamEvent(t, `{"type":"response.output_text.delta","sequence_number":3,"output_index":0,"content_index":0,"item_id":"msg_json","delta":"{\"ok\":true}"}`),
		sdkStreamEvent(t, `{"type":"response.output_text.done","sequence_number":4,"output_index":0,"content_index":0,"item_id":"msg_json","text":"{\"ok\":true}"}`),
		sdkStreamEvent(t, `{"type":"response.content_part.done","sequence_number":5,"output_index":0,"content_index":0,"item_id":"msg_json","part":{"type":"output_text","text":"{\"ok\":true}","annotations":[]}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":6,"output_index":0,"item":{"id":"msg_json","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{\"ok\":true}","annotations":[]}]}}`),
		sdkStreamEvent(t, `{"type":"response.completed","sequence_number":7,"response":{"id":"resp_json","model":"gpt-upstream","status":"completed","usage":{"input_tokens":3,"output_tokens":3,"total_tokens":6}}}`),
	}
}

func TestOpenAIProviderLocalContractCapabilityMatrix(t *testing.T) {
	cases := []struct {
		name       string
		request    string
		events     func(*testing.T) []sdkresponses.ResponseStreamEventUnion
		assertBody func(*testing.T, map[string]any)
	}{
		{
			name:    "remote image",
			request: `{"model":"openai/public","input":[{"role":"user","content":[{"type":"input_text","text":"look"},{"type":"input_image","image_url":"https://example.com/image","detail":"low"}]}]}`,
			events:  textSDKEvents,
			assertBody: func(t *testing.T, body map[string]any) {
				input := arrayField(t, body["input"], "input")
				content := arrayField(t, objectField(t, input[0], "input[0]")["content"], "input[0].content")
				image := objectField(t, content[1], "input[0].content[1]")
				if image["type"] != "input_image" || image["image_url"] != "https://example.com/image" || image["detail"] != "low" {
					t.Fatalf("image = %#v", image)
				}
			},
		},
		{
			name:    "function tool",
			request: `{"model":"openai/public","input":"search","tools":[{"type":"function","name":"Search","parameters":{"type":"object"},"strict":true}],"tool_choice":{"type":"function","name":"Search"}}`,
			events:  toolSDKEvents,
			assertBody: func(t *testing.T, body map[string]any) {
				tool := objectField(t, arrayField(t, body["tools"], "tools")[0], "tools[0]")
				choice := objectField(t, body["tool_choice"], "tool_choice")
				if tool["name"] != "Search" || tool["strict"] != true || choice["name"] != "Search" {
					t.Fatalf("tool/choice = %#v/%#v", tool, choice)
				}
			},
		},
		{
			name:    "structured JSON",
			request: `{"model":"openai/public","input":"json","text":{"format":{"type":"json_schema","name":"Answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}},"strict":true}}}`,
			events:  structuredSDKEvents,
			assertBody: func(t *testing.T, body map[string]any) {
				format := objectField(t, objectField(t, body["text"], "text")["format"], "text.format")
				if format["type"] != "json_schema" || format["strict"] != true {
					t.Fatalf("format = %#v", format)
				}
			},
		},
		{
			name:    "reasoning summary",
			request: `{"model":"openai/public","input":"reason","reasoning":{"summary":"concise"}}`,
			events:  reasoningSDKEvents,
			assertBody: func(t *testing.T, body map[string]any) {
				if objectField(t, body["reasoning"], "reasoning")["summary"] != "concise" {
					t.Fatalf("reasoning = %#v", body["reasoning"])
				}
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			bodies := make(chan map[string]any, 1)
			events := test.events(t)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				var body map[string]any
				decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
				decoder.UseNumber()
				if err := decoder.Decode(&body); err != nil {
					t.Errorf("request JSON: %v", err)
					return
				}
				bodies <- body
				if err := serveSDKEvents(writer, events); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			provider := newContractProvider(t, server, credential.NewEnvSource(func(string) (string, bool) { return "contract-key", true }))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			source, err := provider.Execute(ctx, "gpt-test", decodeCanonicalRequest(t, test.request))
			if err != nil {
				t.Fatal(err)
			}
			response, err := drainProviderSource(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			body := <-bodies
			test.assertBody(t, body)
			if response.Model() != "openai/public" || calls.Load() != 1 {
				t.Fatalf("response/calls = %#v/%d", response, calls.Load())
			}
		})
	}
}

func TestOpenAIProviderLocalContractRejectsMalformedAndSequenceGap(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{
			name:    "malformed SSE",
			payload: "event: response.created\ndata: {\"type\":\"response.created\",\"private-contract-canary\":\"secret\",\"response\":\n\n",
		},
		{
			name: "sequence gap",
			payload: "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_gap\",\"model\":\"gpt-upstream\",\"status\":\"in_progress\"}}\n\n" +
				"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"sequence_number\":2,\"output_index\":0,\"item\":{\"id\":\"msg_gap\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"in_progress\",\"content\":[]}}\n\n",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				writer.Header().Set("Content-Type", "text/event-stream")
				if _, err := io.WriteString(writer, test.payload); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			provider := newContractProvider(t, server, credential.NewEnvSource(func(string) (string, bool) { return "contract-key", true }))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			source, err := provider.Execute(ctx, "gpt-test", textRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = drainProviderSource(ctx, source)
			if execution.CodeOf(err) != execution.CodeInternal || strings.Contains(err.Error(), "private-contract-canary") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("contract error = %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("attempts = %d", calls.Load())
			}
		})
	}
}

func TestOpenAIProviderLocalContractNormalizesHTTPFailuresWithoutRetry(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   execution.ErrorCode
	}{
		{name: "bad request", status: http.StatusBadRequest, code: execution.CodeInvalidRequest},
		{name: "authentication", status: http.StatusUnauthorized, code: execution.CodeAuthenticationFailed},
		{name: "permission", status: http.StatusForbidden, code: execution.CodePermissionDenied},
		{name: "rate limit", status: http.StatusTooManyRequests, code: execution.CodeRateLimited},
		{name: "unavailable", status: http.StatusInternalServerError, code: execution.CodeUnavailable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				writer.Header().Set("Content-Type", "application/json")
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(`{"error":{"message":"private upstream contract canary","type":"server_error","code":"server_error"}}`))
			}))
			defer server.Close()
			source := credential.NewEnvSource(func(string) (string, bool) { return "contract-key", true })
			provider := newContractProvider(t, server, source)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			events, err := provider.Execute(ctx, "gpt-test", textRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = drainProviderSource(ctx, events)
			if execution.CodeOf(err) != test.code || strings.Contains(err.Error(), "private") {
				t.Fatalf("normalized error = %v, code=%q", err, execution.CodeOf(err))
			}
			if calls.Load() != 1 {
				t.Fatalf("attempts = %d", calls.Load())
			}
			if test.status == http.StatusTooManyRequests {
				if !execution.Retryable(err) || execution.RetryAfter(err) != 2*time.Second {
					t.Fatalf("rate limit retry metadata = retryable:%t after:%v", execution.Retryable(err), execution.RetryAfter(err))
				}
			} else if execution.Retryable(err) != (test.status >= 500) {
				t.Fatalf("retryable = %t for status %d", execution.Retryable(err), test.status)
			}
		})
	}
}
