//go:build phase4astress

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/credential"
)

func TestOpenAIProviderConcurrentStress(t *testing.T) {
	const workers = 64
	const rounds = 8
	events := textSDKEvents(t)
	var payload strings.Builder
	for _, event := range events {
		document := event.RawJSON()
		if document == "" {
			t.Fatal("SDK event has no raw JSON")
		}
		_, _ = fmt.Fprintf(&payload, "event: %s\ndata: %s\n\n", event.Type, document)
	}
	wire := payload.String()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(wire))
	}))
	defer server.Close()
	factory, err := newClientFactory(server.URL+"/v1", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(credential.NewEnvSource(func(string) (string, bool) { return "stress-key", true }), openAIRef(t), &sdkStreamStarter{factory: factory}, randomStreamIDs{})
	if err != nil {
		t.Fatal(err)
	}
	request := textRequest(t)
	errorsByWorker := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for round := 0; round < rounds; round++ {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				source, err := provider.Execute(ctx, "gpt-test", request)
				if err == nil {
					_, err = drainProviderSource(ctx, source)
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

func TestStressWireIsValidJSON(t *testing.T) {
	for _, event := range textSDKEvents(t) {
		if !json.Valid([]byte(event.RawJSON())) {
			t.Fatal("stress event is invalid JSON")
		}
	}
}
