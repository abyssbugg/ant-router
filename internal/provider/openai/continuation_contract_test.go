package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/credential"
	inbound "github.com/abyssbugg/ant-router/internal/openai/responses"
	sdkresponses "github.com/openai/openai-go/v3/responses"
)

func twoTurnToolSDKEvents(t *testing.T) []sdkresponses.ResponseStreamEventUnion {
	t.Helper()
	return []sdkresponses.ResponseStreamEventUnion{
		sdkStreamEvent(t, `{"type":"response.created","sequence_number":0,"response":{"id":"resp_turn_one","model":"gpt-test","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"rs_upstream","type":"reasoning","summary":[],"status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_part.added","sequence_number":2,"output_index":0,"item_id":"rs_upstream","summary_index":0,"part":{"type":"summary_text","text":""}}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_text.delta","sequence_number":3,"output_index":0,"item_id":"rs_upstream","summary_index":0,"delta":"need docs"}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_text.done","sequence_number":4,"output_index":0,"item_id":"rs_upstream","summary_index":0,"text":"need docs"}`),
		sdkStreamEvent(t, `{"type":"response.reasoning_summary_part.done","sequence_number":5,"output_index":0,"item_id":"rs_upstream","summary_index":0,"part":{"type":"summary_text","text":"need docs"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":6,"output_index":0,"item":{"id":"rs_upstream","type":"reasoning","summary":[{"type":"summary_text","text":"need docs"}],"encrypted_content":"cipher-two-turn","status":"completed"}}`),
		sdkStreamEvent(t, `{"type":"response.output_item.added","sequence_number":7,"output_index":1,"item":{"id":"fc_upstream","type":"function_call","call_id":"call_upstream","name":"Search","arguments":"","status":"in_progress"}}`),
		sdkStreamEvent(t, `{"type":"response.function_call_arguments.delta","sequence_number":8,"output_index":1,"item_id":"fc_upstream","delta":"{\"query\":\"docs\"}"}`),
		sdkStreamEvent(t, `{"type":"response.function_call_arguments.done","sequence_number":9,"output_index":1,"item_id":"fc_upstream","name":"Search","arguments":"{\"query\":\"docs\"}"}`),
		sdkStreamEvent(t, `{"type":"response.output_item.done","sequence_number":10,"output_index":1,"item":{"id":"fc_upstream","type":"function_call","call_id":"call_upstream","name":"Search","arguments":"{\"query\":\"docs\"}","status":"completed"}}`),
		sdkStreamEvent(t, `{"type":"response.completed","sequence_number":11,"response":{"id":"resp_turn_one","model":"gpt-test","status":"completed","usage":{"input_tokens":5,"output_tokens":4,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":9}}}`),
	}
}

func TestOpenAIProviderLocalContractTwoTurnStatelessToolContinuation(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			t.Errorf("request JSON: %v", err)
			return
		}
		mu.Lock()
		bodies = append(bodies, body)
		turn := len(bodies)
		mu.Unlock()
		events := textSDKEvents(t)
		if turn == 1 {
			events = twoTurnToolSDKEvents(t)
		}
		if err := serveSDKEvents(writer, events); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	provider := newContractProvider(t, server, credential.NewEnvSource(func(string) (string, bool) { return "contract-key", true }))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	firstRequest := decodeCanonicalRequest(t, `{"model":"openai/public","input":"find docs","tools":[{"type":"function","name":"Search","parameters":{"type":"object"},"strict":true}],"tool_choice":{"type":"function","name":"Search"},"reasoning":{"summary":"concise"}}`)
	firstSource, err := provider.Execute(ctx, "gpt-test", firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	firstResponse, err := drainProviderSource(ctx, firstSource)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := inbound.NewEncoder(time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	wireResponse, err := encoder.EncodeResponse(firstResponse)
	if err != nil {
		t.Fatal(err)
	}
	var firstDocument map[string]any
	if err := json.Unmarshal(wireResponse, &firstDocument); err != nil {
		t.Fatal(err)
	}
	input := append([]any(nil), arrayField(t, firstDocument["output"], "output")...)
	call := objectField(t, input[1], "output[1]")
	input = append(input, map[string]any{"type": "function_call_output", "call_id": call["call_id"], "output": "docs found"})
	secondDocument := map[string]any{
		"model": "openai/public", "input": input, "store": false,
		"include":   []string{"reasoning.encrypted_content"},
		"tools":     []any{map[string]any{"type": "function", "name": "Search", "parameters": map[string]any{"type": "object"}, "strict": true}},
		"reasoning": map[string]any{"summary": "concise"},
	}
	secondWire, err := json.Marshal(secondDocument)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := inbound.DecodeWithAffinity(bytes.NewReader(secondWire), &mapperIDs{}, mapperAffinityResolver{providerID: "openai-test", upstreamModel: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	secondSource, err := provider.Execute(ctx, "gpt-test", parsed.Request)
	if err != nil {
		t.Fatal(err)
	}
	secondResponse, err := drainProviderSource(ctx, secondSource)
	if err != nil {
		t.Fatal(err)
	}
	if secondResponse.Model() != "openai/public" || len(secondResponse.Items()) != 1 {
		t.Fatalf("second response = %#v", secondResponse)
	}
	message, ok := secondResponse.Items()[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("second response text = %#v, message=%t", secondResponse.Items(), ok)
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != "hello" {
		t.Fatalf("second response text = %q, text=%t", text.Text(), ok)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("request count = %d, want 2", len(bodies))
	}
	for index, body := range bodies {
		if body["store"] != false {
			t.Fatalf("request %d store = %#v", index, body["store"])
		}
		include := arrayField(t, body["include"], "include")
		if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
			t.Fatalf("request %d include = %#v", index, include)
		}
	}
	replay := arrayField(t, bodies[1]["input"], "input")
	if len(replay) != 3 {
		t.Fatalf("replay length = %d, want 3", len(replay))
	}
	reasoning := objectField(t, replay[0], "input[0]")
	functionCall := objectField(t, replay[1], "input[1]")
	functionOutput := objectField(t, replay[2], "input[2]")
	if reasoning["id"] != "rs_upstream" || reasoning["encrypted_content"] != "cipher-two-turn" {
		t.Fatalf("reasoning replay = %#v", reasoning)
	}
	if functionCall["id"] != "fc_upstream" || functionCall["call_id"] != "call_upstream" {
		t.Fatalf("function call replay = %#v", functionCall)
	}
	if functionOutput["call_id"] != "call_upstream" || functionOutput["output"] != "docs found" {
		t.Fatalf("function output replay = %#v", functionOutput)
	}
}
