//go:build liveopenai

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	inbound "github.com/abyssbugg/ant-router/internal/openai/responses"
)

const (
	liveRequestTimeout = 60 * time.Second
	liveTokenBudget    = int64(2048)
)

func TestLiveOpenAICertification(t *testing.T) {
	if os.Getenv("ANT_ROUTER_LIVE_OPENAI") != "1" {
		t.Skip("live OpenAI certification is not explicitly enabled")
	}
	if value, ok := os.LookupEnv("OPENAI_API_KEY"); !ok || strings.TrimSpace(value) == "" {
		t.Fatal("OPENAI_API_KEY is required for live certification")
	}
	upstreamModel := strings.TrimSpace(os.Getenv("ANT_ROUTER_LIVE_OPENAI_MODEL"))
	if upstreamModel == "" {
		t.Fatal("ANT_ROUTER_LIVE_OPENAI_MODEL is required for live certification")
	}
	ref, err := credential.NewRef(credential.SourceEnv, "OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(credential.NewEnvSource(nil), ref)
	if err != nil {
		t.Fatal(err)
	}

	tool := decodeCanonicalRequest(t, `{"model":"openai/live","input":"Call Ping with an empty JSON object.","tools":[{"type":"function","name":"Ping","description":"Return a health acknowledgement.","parameters":{"type":"object","properties":{},"additionalProperties":false},"strict":true}],"tool_choice":{"type":"function","name":"Ping"},"reasoning":{"summary":"concise"},"max_output_tokens":64}`)
	toolResponse := executeLiveRequest(t, provider, upstreamModel, tool)
	if !hasSingleToolCall(toolResponse.Items(), "Ping") {
		t.Fatal("live tool response did not contain exactly one required function call")
	}
	if _, ok := toolResponse.Continuation(); !ok {
		t.Fatal("live tool response did not contain stateless continuation")
	}
	followUp := liveContinuationRequest(t, toolResponse, upstreamModel)
	finalResponse := executeLiveRequest(t, provider, upstreamModel, followUp)
	if !hasMessage(finalResponse.Items()) {
		t.Fatal("live continuation response did not contain an assistant message")
	}

	total := toolResponse.Usage().TotalTokens() + finalResponse.Usage().TotalTokens()
	if total <= 0 || total > liveTokenBudget {
		t.Fatalf("live usage exceeded certification bounds: total_tokens=%d", total)
	}
}

func liveContinuationRequest(t *testing.T, response canonical.Response, upstreamModel string) canonical.Request {
	t.Helper()
	encoder, err := inbound.NewEncoder(time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encoder.EncodeResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	input, ok := document["output"].([]any)
	if !ok {
		t.Fatal("live response output is invalid")
	}
	input = append([]any(nil), input...)
	callID := ""
	callCount := 0
	for _, value := range input {
		item, ok := value.(map[string]any)
		if ok && item["type"] == "function_call" {
			callCount++
			callID, _ = item["call_id"].(string)
		}
	}
	if callCount != 1 || callID == "" {
		t.Fatal("live response did not contain exactly one function call identity")
	}
	input = append(input, map[string]any{"type": "function_call_output", "call_id": callID, "output": "pong"})
	requestDocument := map[string]any{
		"model": "openai/live", "input": input, "store": false,
		"include":   []string{"reasoning.encrypted_content"},
		"tools":     []any{map[string]any{"type": "function", "name": "Ping", "description": "Return a health acknowledgement.", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, "strict": true}},
		"reasoning": map[string]any{"summary": "concise"}, "max_output_tokens": 64,
	}
	wire, err := json.Marshal(requestDocument)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := inbound.DecodeWithAffinity(bytes.NewReader(wire), &mapperIDs{}, mapperAffinityResolver{providerID: "openai", upstreamModel: upstreamModel})
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Request
}

func executeLiveRequest(t *testing.T, provider *Provider, upstreamModel string, request canonical.Request) canonical.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), liveRequestTimeout)
	defer cancel()
	source, err := provider.Execute(ctx, upstreamModel, request)
	if err != nil {
		t.Fatalf("live provider execution failed: %v", err)
	}
	response, err := drainProviderSource(ctx, source)
	if err != nil {
		t.Fatalf("live provider stream failed: %v", err)
	}
	if !response.Valid() || response.Model() != request.Model() {
		t.Fatal("live provider returned an invalid canonical response")
	}
	return response
}

func hasMessage(items []canonical.Item) bool {
	for _, item := range items {
		if _, ok := item.Message(); ok {
			return true
		}
	}
	return false
}

func hasSingleToolCall(items []canonical.Item, name string) bool {
	count := 0
	for _, item := range items {
		if call, ok := item.ToolCall(); ok {
			if call.Name() != name {
				return false
			}
			count++
		}
	}
	return count == 1
}
