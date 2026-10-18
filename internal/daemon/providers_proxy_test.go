package daemon

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/credential"
)

// TestProviderRegistrationHonorsConfiguredProxy decodes a schema v8 provider
// whose upstream is an unreachable loopback address and whose proxy answers
// as the upstream: execution succeeds only if the daemon wires the proxy.
func TestProviderRegistrationHonorsConfiguredProxy(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxied.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"id\":\"c1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"+
			"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{}}]}\n\n"+
			"data: {\"id\":\"c1\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer proxy.Close()

	document := `schema_version = 8
[[providers]]
id = "local"
kind = "openai"
enabled = true
base_url = "http://127.0.0.1:1/v1"
wire = "chat"
proxy = "` + proxy.URL + `"
[providers.credential]
source = "env"
name = "OPENAI_LOCAL_KEY"
[[providers.models]]
alias = "local/m"
upstream = "m"
capabilities = ["text"]
`
	cfg, err := config.Decode(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	registration, err := buildProviderRegistration(cfg.Providers[0], credential.NewEnvSource(func(string) (string, bool) { return "k", true }))
	if err != nil {
		t.Fatal(err)
	}
	source, err := registration.Adapter.Execute(context.Background(), "m", proxyTestRequest(t))
	if err != nil {
		t.Fatalf("Execute() = %v; proxy not wired", err)
	}
	_ = source.Close()
	if proxied.Load() != 1 {
		t.Fatalf("proxy saw %d requests, want 1", proxied.Load())
	}
}

func proxyTestRequest(t *testing.T) canonical.Request {
	t.Helper()
	itemID, err := canonical.GenerateItemID(bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := canonical.GenerateRequestID(bytes.NewReader(bytes.Repeat([]byte{2}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	text, err := canonical.NewTextContent("hello")
	if err != nil {
		t.Fatal(err)
	}
	block, err := canonical.NewTextBlock(text)
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessage(itemID, canonical.Role("user"), []canonical.ContentBlock{block})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewMessageItem(message)
	if err != nil {
		t.Fatal(err)
	}
	request, err := canonical.NewRequest(canonical.RequestParams{ID: requestID, Model: "local/m", Input: []canonical.Item{item}})
	if err != nil {
		t.Fatal(err)
	}
	return request
}
