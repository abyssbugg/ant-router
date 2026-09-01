//go:build liveopenai

package openai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
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

	text := decodeCanonicalRequest(t, `{"model":"openai/live","input":"Reply with a short acknowledgement.","max_output_tokens":64}`)
	textResponse := executeLiveRequest(t, provider, upstreamModel, text)
	if !hasMessage(textResponse.Items()) {
		t.Fatal("live text response did not contain an assistant message")
	}

	tool := decodeCanonicalRequest(t, `{"model":"openai/live","input":"Call Ping with an empty JSON object.","tools":[{"type":"function","name":"Ping","description":"Return a health acknowledgement.","parameters":{"type":"object","properties":{},"additionalProperties":false},"strict":true}],"tool_choice":{"type":"function","name":"Ping"},"max_output_tokens":64}`)
	toolResponse := executeLiveRequest(t, provider, upstreamModel, tool)
	if !hasToolCall(toolResponse.Items(), "Ping") {
		t.Fatal("live tool response did not contain the required function call")
	}

	total := textResponse.Usage().TotalTokens() + toolResponse.Usage().TotalTokens()
	if total <= 0 || total > liveTokenBudget {
		t.Fatalf("live usage exceeded certification bounds: total_tokens=%d", total)
	}
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

func hasToolCall(items []canonical.Item, name string) bool {
	for _, item := range items {
		if call, ok := item.ToolCall(); ok && call.Name() == name {
			return true
		}
	}
	return false
}
