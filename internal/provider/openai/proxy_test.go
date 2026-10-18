package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/abyssbugg/ant-router/internal/credential"
)

// TestChatWireRoutesThroughConfiguredProxy points the provider at a loopback
// upstream nothing listens on; the request can only succeed if the transport
// sends it to the configured proxy, which answers as the upstream.
func TestChatWireRoutesThroughConfiguredProxy(t *testing.T) {
	var proxied atomic.Int32
	var requestURI atomic.Value
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxied.Add(1)
		requestURI.Store(request.RequestURI)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, chatWireSSE(chatWireEvents()))
	}))
	defer proxy.Close()

	provider, err := NewProviderForIDWithEndpoint("deepseek", credential.NewEnvSource(func(string) (string, bool) { return "k", true }), mustChatCredentialRef(t), Endpoint{BaseURL: "http://127.0.0.1:1/v1", Chat: true, Proxy: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	source, err := provider.Execute(context.Background(), "deepseek-chat", chatWireRequest(t))
	if err != nil {
		t.Fatalf("Execute() through proxy = %v", err)
	}
	_ = source.Close()
	if proxied.Load() != 1 {
		t.Fatalf("proxy saw %d requests, want 1", proxied.Load())
	}
	if got, _ := requestURI.Load().(string); got != "http://127.0.0.1:1/v1/chat/completions" {
		t.Fatalf("proxy request URI = %q, want absolute upstream URL", got)
	}
}

func TestEndpointRejectsInvalidProxy(t *testing.T) {
	_, err := NewProviderForIDWithEndpoint("deepseek", credential.NewEnvSource(func(string) (string, bool) { return "k", true }), mustChatCredentialRef(t), Endpoint{BaseURL: "http://127.0.0.1:1/v1", Chat: true, Proxy: "http://user:secret@127.0.0.1:3128"})
	if err == nil {
		t.Fatal("provider accepted a proxy URL with credentials")
	}
}
