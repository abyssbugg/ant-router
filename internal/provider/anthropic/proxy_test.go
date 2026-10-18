package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/abyssbugg/ant-router/internal/credential"
)

// TestProviderTunnelsOfficialEndpointThroughProxy verifies the public
// constructor routes the official HTTPS endpoint through the configured proxy
// as a CONNECT tunnel, so upstream TLS is still verified end to end.
func TestProviderTunnelsOfficialEndpointThroughProxy(t *testing.T) {
	var mu sync.Mutex
	var method, host string
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		method, host = request.Method, request.Host
		mu.Unlock()
		http.Error(writer, "tunnel refused by test proxy", http.StatusForbidden)
	}))
	defer proxy.Close()

	ref, err := credential.NewRef(credential.SourceEnv, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProviderWithOptions(&providerCredentialSource{value: "private-key"}, ref, anthropicProviderLimits(), Options{Proxy: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	source, err := provider.Execute(context.Background(), anthropicProviderLimits()[0].Upstream, anthropicTextRequest(t))
	if err == nil {
		// The SDK surfaces transport failures on the first read.
		for i := 0; i < 8 && err == nil; i++ {
			_, err = source.Recv(context.Background())
		}
		_ = source.Close()
	}
	if err == nil {
		t.Fatal("stream started although the proxy refused the tunnel")
	}
	mu.Lock()
	defer mu.Unlock()
	if method != http.MethodConnect || host != "api.anthropic.com:443" {
		t.Fatalf("proxy saw %s %s, want CONNECT api.anthropic.com:443", method, host)
	}
}

func TestNewProviderWithOptionsRejectsInvalidProxy(t *testing.T) {
	ref, err := credential.NewRef(credential.SourceEnv, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProviderWithOptions(&providerCredentialSource{value: "k"}, ref, anthropicProviderLimits(), Options{Proxy: "ftp://127.0.0.1:21"}); err == nil {
		t.Fatal("provider accepted an unsupported proxy scheme")
	}
}
