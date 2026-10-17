package dataplane

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/canonical"
	"github.com/abyssbugg/ant-router/internal/execution"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
	"github.com/abyssbugg/ant-router/internal/streaming"
)

type continuationBackend struct{ continuation *canonical.Continuation }

func (b *continuationBackend) ResolveModel(alias string) (string, string, error) {
	return "openai-primary", "gpt-upstream", nil
}
func (b *continuationBackend) Execute(_ context.Context, request canonical.Request) (streaming.EventSource, error) {
	b.continuation = request.Continuation()
	return nil, execution.NewError(execution.CodeUnavailable, "provider", 0, nil)
}
func (b *continuationBackend) Models(context.Context) ([]execution.Model, error) { return nil, nil }

func TestHandlerWithoutAffinityResolverReturnsRedactedInternalError(t *testing.T) {
	handler, token := testHandler(t)
	const marker = "private-continuation-marker"
	body := `{"model":"mock/text","input":[{"type":"reasoning","id":"rs_upstream","summary":[],"encrypted_content":"` + marker + `"}]}`
	recorder := serve(t, handler, token, http.MethodPost, "/v1/responses", body)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), marker) {
		t.Fatal("internal response leaked continuation content")
	}
}

func TestHandlerBindsReplayedReasoningThroughAffinityResolver(t *testing.T) {
	token := inferencetoken.Token{}
	for index := range token {
		token[index] = 9
	}
	backend := &continuationBackend{}
	handler, err := NewHandler(HandlerOptions{
		TokenSource: StaticTokenSource(token), Executor: backend, Catalog: backend, AffinityResolver: backend,
		ListenAddress: "127.0.0.1:8787", MaxConcurrent: 1, RequestTimeout: time.Second,
		Clock: func() time.Time { return time.Unix(1, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"openai/public","input":[{"type":"reasoning","id":"rs_upstream","summary":[],"encrypted_content":"cipher"}]}`
	_ = serve(t, handler, token, http.MethodPost, "/v1/responses", body)
	if backend.continuation == nil {
		t.Fatal("executor received no continuation")
	}
	if backend.continuation.Provider() != "openai-primary" || backend.continuation.Model() != "gpt-upstream" {
		t.Fatalf("affinity = %q/%q", backend.continuation.Provider(), backend.continuation.Model())
	}
}

var _ execution.Executor = (*continuationBackend)(nil)
var _ execution.ModelCatalog = (*continuationBackend)(nil)

type generationRuntime struct {
	generation int
	resolved   *int
	executed   *int
}

func (r generationRuntime) ResolveModel(string) (string, string, error) {
	*r.resolved = r.generation
	return "openai-primary", "gpt-upstream", nil
}

func (r generationRuntime) Execute(context.Context, canonical.Request) (streaming.EventSource, error) {
	*r.executed = r.generation
	return nil, execution.NewError(execution.CodeUnavailable, "provider", 0, nil)
}

// A configuration activation can swap the provider runtime at any moment, so
// affinity resolution and execution must use one runtime per request.
func TestHandlerPinsOneRuntimeForAffinityAndExecution(t *testing.T) {
	token := inferencetoken.Token{}
	for index := range token {
		token[index] = 9
	}
	var resolved, executed, generations int
	backend := &continuationBackend{}
	handler, err := NewHandler(HandlerOptions{
		TokenSource: StaticTokenSource(token), Executor: backend, Catalog: backend, AffinityResolver: backend,
		Runtime: func() RequestRuntime {
			generations++
			return generationRuntime{generation: generations, resolved: &resolved, executed: &executed}
		},
		ListenAddress: "127.0.0.1:8787", MaxConcurrent: 1, RequestTimeout: time.Second,
		Clock: func() time.Time { return time.Unix(1, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"openai/public","input":[{"type":"reasoning","id":"rs_upstream","summary":[],"encrypted_content":"cipher"}]}`
	_ = serve(t, handler, token, http.MethodPost, "/v1/responses", body)
	if resolved == 0 || resolved != executed || generations != 1 {
		t.Fatalf("resolved on generation %d, executed on %d, pinned %d runtimes; want one shared runtime", resolved, executed, generations)
	}
	if backend.continuation != nil {
		t.Fatal("request bypassed the pinned runtime")
	}
}
