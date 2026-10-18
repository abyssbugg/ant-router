package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abyssbugg/ant-router/internal/credential"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/provider/keypool"
)

// TestKeyPoolFailsOverOnFirstReadRateLimit covers the lazy SDK path: the
// Anthropic stream reports the upstream 429 on its first read, which the pool
// must still treat as a pre-response failure and retry on the next key.
func TestKeyPoolFailsOverOnFirstReadRateLimit(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		key := request.Header.Get("X-Api-Key")
		mu.Lock()
		keys = append(keys, key)
		mu.Unlock()
		if key == "key-a" {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
			return
		}
		serveAnthropicTextStream(writer)
	}))
	defer server.Close()

	members := make([]execution.ProviderAdapter, 0, 2)
	for _, key := range []string{"key-a", "key-b"} {
		ref, err := credential.NewRef(credential.SourceEnv, "ANTHROPIC_API_KEY")
		if err != nil {
			t.Fatal(err)
		}
		factory, err := newClientFactory(server.URL, newSecureHTTPClient())
		if err != nil {
			t.Fatal(err)
		}
		provider, err := newProvider(&providerCredentialSource{value: key}, ref, anthropicProviderLimits(), &sdkStreamStarter{factory: factory}, &sourceIDs{})
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, provider)
	}
	pool, err := keypool.New(members)
	if err != nil {
		t.Fatal(err)
	}
	source, err := pool.Execute(context.Background(), anthropicProviderLimits()[0].Upstream, anthropicTextRequest(t))
	if err != nil {
		t.Fatalf("Execute() = %v; pool did not fail over on first-read 429", err)
	}
	defer func() { _ = source.Close() }()
	events := 0
	for {
		if _, err := source.Recv(context.Background()); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Recv() = %v", err)
			}
			break
		}
		events++
	}
	if events == 0 {
		t.Fatal("no events delivered from the fallback key")
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(keys, ",") != "key-a,key-b" {
		t.Fatalf("upstream saw keys %v, want key-a then key-b", keys)
	}
}
