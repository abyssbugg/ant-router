package anthropic

import (
	"context"
	"crypto/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/dataplane"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	openairesponses "github.com/openai/openai-go/v3/responses"
)

func TestOfficialOpenAISDKRoutesThroughAnthropicProvider(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" || request.Header.Get("X-Api-Key") != "private-anthropic-key" {
			http.Error(writer, "rejected", http.StatusUnauthorized)
			return
		}
		serveAnthropicTextStream(writer)
	}))
	defer upstream.Close()

	ref, err := credential.NewRef(credential.SourceEnv, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	factory, err := newClientFactory(upstream.URL, newSecureHTTPClient())
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(credential.NewEnvSource(func(name string) (string, bool) { return "private-anthropic-key", name == "ANTHROPIC_API_KEY" }), ref, anthropicProviderLimits(), &sdkStreamStarter{factory: factory}, &sourceIDs{})
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := canonical.NewCapabilitySet(canonical.CapabilityText)
	if err != nil {
		t.Fatal(err)
	}
	model, err := execution.NewModel(execution.ModelParams{ID: "anthropic/public", Provider: "anthropic-primary", Capabilities: capabilities})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := execution.NewRegistry([]execution.Registration{{ProviderID: "anthropic-primary", Adapter: provider, Models: []execution.ModelBinding{{Model: model, Upstream: "claude-upstream"}}}})
	if err != nil {
		t.Fatal(err)
	}

	baseURL, token, stop := startAnthropicSDKDataPlane(t, registry)
	defer stop()
	client := openai.NewClient(option.WithAPIKey(token.String()), option.WithBaseURL(baseURL), option.WithMaxRetries(0))
	params := openairesponses.ResponseNewParams{Model: "anthropic/public", Input: openairesponses.ResponseNewParamsInputUnion{OfString: param.NewOpt("hello")}}
	responseContext, cancelResponse := context.WithTimeout(context.Background(), 10*time.Second)
	response, err := client.Responses.New(responseContext, params)
	cancelResponse()
	if err != nil {
		t.Fatal(err)
	}
	if response == nil || response.Status != "completed" || response.Model != "anthropic/public" || response.OutputText() != "hello" {
		t.Fatalf("response = %#v", response)
	}

	streamContext, cancelStream := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStream()
	stream := client.Responses.NewStreaming(streamContext, params)
	var text string
	seenCompleted := false
	for stream.Next() {
		event := stream.Current()
		if event.Type == "response.output_text.delta" {
			text += event.Delta
		}
		if event.Type == "response.completed" {
			seenCompleted = true
		}
	}
	if err := stream.Err(); err != nil {
		_ = stream.Close()
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if text != "hello" || !seenCompleted {
		t.Fatalf("stream text/completed = %q/%t", text, seenCompleted)
	}
}

func startAnthropicSDKDataPlane(t *testing.T, registry *execution.Registry) (string, inferencetoken.Token, func()) {
	t.Helper()
	token, err := inferencetoken.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	handler, err := dataplane.NewHandler(dataplane.HandlerOptions{TokenSource: dataplane.StaticTokenSource(token), Executor: registry, Catalog: registry, ListenAddress: address, MaxConcurrent: 4, RequestTimeout: 5 * time.Second, Clock: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }})
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	server, err := dataplane.NewServer(listener, handler, dataplane.DefaultServerOptions())
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	<-server.Ready()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		if err := server.Wait(); err != nil {
			t.Errorf("Wait: %v", err)
		}
	}
	return "http://" + address + "/v1", token, stop
}
