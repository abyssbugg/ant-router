//go:build phase4cstress

package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/credential"
)

func TestAnthropicProviderConcurrentStress(t *testing.T) {
	const workers = 64
	const rounds = 8
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		serveAnthropicTextStream(writer)
	}))
	defer server.Close()
	factory, err := newClientFactory(server.URL, newSecureHTTPClient())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := credential.NewRef(credential.SourceEnv, "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(credential.NewEnvSource(func(string) (string, bool) { return "stress-key", true }), ref, anthropicProviderLimits())
	if err != nil {
		t.Fatal(err)
	}
	provider.starter = &sdkStreamStarter{factory: factory}
	request := anthropicTextRequest(t)
	errorsByWorker := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for round := 0; round < rounds; round++ {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				source, err := provider.Execute(ctx, "claude-upstream", request)
				if err == nil {
					_, err = drainAnthropicProvider(ctx, source)
				}
				cancel()
				if err != nil {
					errorsByWorker <- err
					return
				}
			}
		}()
	}
	group.Wait()
	close(errorsByWorker)
	if err := <-errorsByWorker; err != nil {
		t.Fatalf("concurrent provider execution failed: %v", err)
	}
	if want := int64(workers * rounds); calls.Load() != want {
		t.Fatalf("request count = %d, want %d", calls.Load(), want)
	}
}
