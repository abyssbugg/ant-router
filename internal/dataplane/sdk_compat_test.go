package dataplane_test

import (
	"context"
	"crypto/rand"
	"net"
	"strings"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	openairesponses "github.com/openai/openai-go/v3/responses"

	"github.com/abyssbugg/ant-router/internal/dataplane"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/provider/mock"
)

func TestOfficialOpenAISDKModelsAndResponses(t *testing.T) {
	baseURL, token, stop := startSDKServer(t)
	defer stop()
	client := openai.NewClient(option.WithAPIKey(token.String()), option.WithBaseURL(baseURL))

	models, err := client.Models.List(context.Background())
	if err != nil {
		t.Fatalf("Models.List() failed: %v", err)
	}
	if len(models.Data) != 6 || models.Data[0].ID != "mock/disconnect" || models.Data[5].ID != "mock/tool" {
		t.Fatalf("models = %#v", models.Data)
	}

	params := openairesponses.ResponseNewParams{
		Model: "mock/text",
		Input: openairesponses.ResponseNewParamsInputUnion{OfString: param.NewOpt("hello")},
	}
	response, err := client.Responses.New(context.Background(), params)
	if err != nil {
		t.Fatalf("Responses.New() failed: %v", err)
	}
	if response == nil {
		t.Fatal("Responses.New() returned a nil response without an error")
	}
	if response.Status != "completed" || response.Model != "mock/text" || response.OutputText() != "mock response" {
		t.Fatalf("response = status %q model %q text %q", response.Status, response.Model, response.OutputText())
	}

	stream := client.Responses.NewStreaming(context.Background(), params)
	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			t.Errorf("stream Close() failed: %v", closeErr)
		}
	}()
	var delta strings.Builder
	seenCreated := false
	seenCompleted := false
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.created":
			seenCreated = true
		case "response.output_text.delta":
			delta.WriteString(event.Delta)
		case "response.completed":
			seenCompleted = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("Responses.NewStreaming() failed: %v", err)
	}
	if !seenCreated || !seenCompleted || delta.String() != "mock response" {
		t.Fatalf("stream = created %t completed %t delta %q", seenCreated, seenCompleted, delta.String())
	}
}

func startSDKServer(t *testing.T) (string, inferencetoken.Token, func()) {
	t.Helper()
	token, err := inferencetoken.Generate(rand.Reader)
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	provider, err := mock.NewRandom(time.Millisecond)
	if err != nil {
		t.Fatalf("NewRandom() failed: %v", err)
	}
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	address := listener.Addr().String()
	handler, err := dataplane.NewHandler(dataplane.HandlerOptions{
		TokenSource:    dataplane.StaticTokenSource(token),
		Executor:       provider,
		Catalog:        provider,
		ListenAddress:  address,
		MaxConcurrent:  4,
		RequestTimeout: 5 * time.Second,
		Clock:          func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	})
	if err != nil {
		_ = listener.Close()
		t.Fatalf("NewHandler() failed: %v", err)
	}
	server, err := dataplane.NewServer(listener, handler, dataplane.DefaultServerOptions())
	if err != nil {
		_ = listener.Close()
		t.Fatalf("NewServer() failed: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	<-server.Ready()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() failed: %v", err)
		}
		if err := server.Wait(); err != nil {
			t.Errorf("Wait() failed: %v", err)
		}
	}
	return "http://" + address + "/v1", token, stop
}
