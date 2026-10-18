package daemon

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abyssbugg/ant-router/internal/config"
	"github.com/abyssbugg/ant-router/internal/credential"
)

// TestProviderRegistrationFailsOverAcrossCredentialPool configures a v8
// credential pool; the upstream rate-limits key A and serves key B, so the
// request must succeed and the next request must skip the cooling key A.
func TestProviderRegistrationFailsOverAcrossCredentialPool(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		key := request.Header.Get("Authorization")
		mu.Lock()
		seen = append(seen, key)
		mu.Unlock()
		if key == "Bearer key-a" {
			writer.Header().Set("Retry-After", "30")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"error":{"message":"slow down"}}`)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"id\":\"c1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"+
			"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{}}]}\n\n"+
			"data: {\"id\":\"c1\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	document := `schema_version = 8
[[providers]]
id = "local"
kind = "openai"
enabled = true
base_url = "` + upstream.URL + `/v1"
wire = "chat"
[[providers.credentials]]
source = "env"
name = "OPENAI_KEY_A"
[[providers.credentials]]
source = "env"
name = "OPENAI_KEY_B"
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
	secrets := map[string]string{"OPENAI_KEY_A": "key-a", "OPENAI_KEY_B": "key-b"}
	registration, err := buildProviderRegistration(cfg.Providers[0], credential.NewEnvSource(func(name string) (string, bool) {
		value, ok := secrets[name]
		return value, ok
	}))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		source, err := registration.Adapter.Execute(context.Background(), "m", proxyTestRequest(t))
		if err != nil {
			t.Fatalf("request %d: Execute() = %v; pool did not fail over", i, err)
		}
		_ = source.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"Bearer key-a", "Bearer key-b", "Bearer key-b"}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("upstream saw keys %v, want %v", seen, want)
	}
}
