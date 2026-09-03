package openai

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdkopenai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	sdkresponses "github.com/openai/openai-go/v3/responses"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/dataplane"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type continuationSDKBackend struct{ provider *Provider }

func (b continuationSDKBackend) Execute(ctx context.Context, request canonical.Request) (streaming.EventSource, error) {
	return b.provider.Execute(ctx, "gpt-test", request)
}
func (continuationSDKBackend) Models(context.Context) ([]execution.Model, error) { return nil, nil }
func (continuationSDKBackend) ResolveModel(string) (string, string, error) {
	return "openai-test", "gpt-test", nil
}

func TestOfficialSDKTwoTurnStatelessToolContinuation(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
	defer upstream.Close()

	provider := newContractProvider(t, upstream, credential.NewEnvSource(func(string) (string, bool) { return "contract-key", true }))
	backend := continuationSDKBackend{provider: provider}
	token, err := inferencetoken.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	handler, err := dataplane.NewHandler(dataplane.HandlerOptions{
		TokenSource: dataplane.StaticTokenSource(token), Executor: backend, Catalog: backend, AffinityResolver: backend,
		ListenAddress: address, MaxConcurrent: 2, RequestTimeout: 5 * time.Second,
		Clock: func() time.Time { return time.Unix(1700000000, 0) },
	})
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
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		if err := server.Wait(); err != nil {
			t.Errorf("Wait: %v", err)
		}
	}()

	client := sdkopenai.NewClient(option.WithAPIKey(token.String()), option.WithBaseURL("http://"+address+"/v1"), option.WithMaxRetries(0))
	tool := sdkresponses.ToolParamOfFunction("Search", map[string]any{"type": "object"}, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err := client.Responses.New(ctx, sdkresponses.ResponseNewParams{
		Model: "openai/public", Input: sdkresponses.ResponseNewParamsInputUnion{OfString: param.NewOpt("find docs")}, Tools: []sdkresponses.ToolUnionParam{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || len(first.Output) != 2 || first.Output[0].Type != "reasoning" || first.Output[1].Type != "function_call" {
		t.Fatalf("first response output = %#v", first)
	}
	reasoningItem := first.Output[0].AsReasoning()
	functionCallItem := first.Output[1].AsFunctionCall()
	if reasoningItem.ID != "rs_upstream" || reasoningItem.EncryptedContent != "cipher-two-turn" || functionCallItem.ID != "fc_upstream" || functionCallItem.CallID != "call_upstream" {
		t.Fatalf("SDK first-turn identity = reasoning:%#v call:%#v", reasoningItem, functionCallItem)
	}
	reasoning := reasoningItem.ToParam()
	functionCall := functionCallItem.ToParam()
	functionOutput := sdkresponses.ResponseInputItemParamOfFunctionCallOutput("docs found")
	functionOutput.OfFunctionCallOutput.CallID = param.NewOpt(functionCallItem.CallID)
	secondInput := []sdkresponses.ResponseInputItemUnionParam{
		{OfReasoning: &reasoning}, {OfFunctionCall: &functionCall}, functionOutput,
	}
	secondParams := sdkresponses.ResponseNewParams{
		Model: "openai/public", Input: sdkresponses.ResponseNewParamsInputUnion{OfInputItemList: secondInput}, Tools: []sdkresponses.ToolUnionParam{tool},
	}
	encodedSecondParams, err := json.Marshal(secondParams)
	if err != nil {
		t.Fatal(err)
	}
	var marshaledSecond map[string]any
	if err := json.Unmarshal(encodedSecondParams, &marshaledSecond); err != nil {
		t.Fatal(err)
	}
	marshaledInput := arrayField(t, marshaledSecond["input"], "SDK input")
	marshaledOutput := objectField(t, marshaledInput[2], "SDK input[2]")
	if marshaledOutput["call_id"] != "call_upstream" {
		t.Fatalf("SDK marshaled function output = %#v", marshaledOutput)
	}
	second, err := client.Responses.New(ctx, secondParams)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.Status != "completed" || second.OutputText() != "hello" {
		t.Fatalf("second response = %#v", second)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("request count = %d, want 2", len(bodies))
	}
	for index, body := range bodies {
		if body["store"] != false {
			t.Fatalf("SDK request %d store = %#v", index, body["store"])
		}
		include := arrayField(t, body["include"], "include")
		if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
			t.Fatalf("SDK request %d include = %#v", index, include)
		}
	}
	replay := arrayField(t, bodies[1]["input"], "input")
	if len(replay) != 3 {
		t.Fatalf("replay length = %d, want 3", len(replay))
	}
	reasoningWire := objectField(t, replay[0], "input[0]")
	callWire := objectField(t, replay[1], "input[1]")
	outputWire := objectField(t, replay[2], "input[2]")
	if reasoningWire["id"] != "rs_upstream" || reasoningWire["encrypted_content"] != "cipher-two-turn" || callWire["id"] != "fc_upstream" || callWire["call_id"] != "call_upstream" || outputWire["call_id"] != "call_upstream" {
		t.Fatalf("SDK replay identity = %#v", replay)
	}
}

var _ execution.Executor = continuationSDKBackend{}
var _ execution.ModelCatalog = continuationSDKBackend{}
