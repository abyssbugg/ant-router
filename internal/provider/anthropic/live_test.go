//go:build liveanthropic

package anthropic

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
)

func TestLiveAnthropicCertification(t *testing.T) {
	if os.Getenv("ANT_ROUTER_LIVE_ANTHROPIC") != "1" {
		t.Skip("set ANT_ROUTER_LIVE_ANTHROPIC=1 to run billable certification")
	}
	upstream := os.Getenv("ANT_ROUTER_LIVE_ANTHROPIC_MODEL")
	if upstream == "" {
		t.Fatal("ANT_ROUTER_LIVE_ANTHROPIC_MODEL is required")
	}
	ref, err := credential.NewRef(credential.SourceEnv, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(credential.NewEnvSource(os.LookupEnv), ref, []ModelLimits{{Upstream: upstream, DefaultMaxOutputTokens: 64, MaxOutputTokens: 64}})
	if err != nil {
		t.Fatal(err)
	}

	textRequest := decodeRequest(t, `{"model":"anthropic/live","input":"Reply with a brief acknowledgement.","max_output_tokens":64}`)
	textResponse := executeLiveAnthropic(t, provider, upstream, textRequest)
	if len(textResponse.Items()) == 0 {
		t.Fatal("live text response contains no output items")
	}

	toolRequest := decodeRequest(t, `{"model":"anthropic/live","input":"Call Ping exactly once.","max_output_tokens":64,"tools":[{"type":"function","name":"Ping","description":"Return a ping.","parameters":{"type":"object","properties":{},"additionalProperties":false},"strict":true}],"tool_choice":{"type":"function","name":"Ping"}}`)
	toolResponse := executeLiveAnthropic(t, provider, upstream, toolRequest)
	calls := 0
	for _, item := range toolResponse.Items() {
		if call, ok := item.ToolCall(); ok {
			calls++
			if call.Name() != "Ping" {
				t.Fatal("live tool response returned the wrong tool")
			}
		}
	}
	if calls != 1 {
		t.Fatalf("live tool call count = %d", calls)
	}
}

func executeLiveAnthropic(t *testing.T, provider *Provider, upstream string, request canonical.Request) canonical.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	source, err := provider.Execute(ctx, upstream, request)
	if err != nil {
		t.Fatal("live Anthropic request failed")
	}
	response, err := drainAnthropicProvider(ctx, source)
	if err != nil {
		t.Fatal("live Anthropic stream failed")
	}
	if !response.Valid() || response.Model() != request.Model() {
		t.Fatal("live Anthropic response is invalid")
	}
	return response
}
