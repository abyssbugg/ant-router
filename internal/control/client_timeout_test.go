package control

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/controltoken"
)

// clientSetupMargin is the minimum headroom the client budget must leave
// beyond the server's full write budget for client-side work that shares the
// same deadline: token retrieval, dialing, and request transit.
const clientSetupMargin = 2 * time.Second

// TestDefaultClientTimeoutCoversServerRequestBudget guards against the client
// abandoning requests the server is still allowed to finish. The server gives
// a handler ReadTimeout to complete (RequestTimeout middleware) and may spend
// up to WriteTimeout writing the response; durable config writes fsync on the
// request path and can be slow on loaded hosts. A shorter client budget turns
// a slow-but-successful mutation into a spurious client failure.
func TestDefaultClientTimeoutCoversServerRequestBudget(t *testing.T) {
	client := DefaultClientOptions()
	server := DefaultServerOptions()
	if client.RequestTimeout <= server.ReadTimeout+clientSetupMargin {
		t.Fatalf("client RequestTimeout %s must exceed server request budget %s by at least %s", client.RequestTimeout, server.ReadTimeout, clientSetupMargin)
	}
	if client.RequestTimeout <= server.WriteTimeout+clientSetupMargin {
		t.Fatalf("client RequestTimeout %s must exceed server WriteTimeout %s by at least %s", client.RequestTimeout, server.WriteTimeout, clientSetupMargin)
	}
}

// TestDefaultClientRequestDeadlineCoversServerBudget runs a real request
// through a default client and checks the deadline it actually enforces.
// Token retrieval, dialing, the handler, and the response read all share one
// request deadline (token retrieval is the first step and receives the request
// context directly), so the time remaining when the request starts must still
// cover the server's full write budget plus setup margin.
func TestDefaultClientRequestDeadlineCoversServerBudget(t *testing.T) {
	token, err := controltoken.Generate(nil)
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// A handler that is slow relative to local IPC must still succeed.
		time.Sleep(50 * time.Millisecond)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok","api_version":"` + APIVersion + `"}`))
	})
	dialer := &handlerDialer{handler: handler}
	t.Cleanup(dialer.Close)
	var remaining time.Duration
	var hasDeadline bool
	tokens := tokenSourceFunc(func(ctx context.Context) (controltoken.Token, error) {
		var deadline time.Time
		deadline, hasDeadline = ctx.Deadline()
		remaining = time.Until(deadline)
		return token, nil
	})
	client, err := NewClient(dialer, tokens, DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	t.Cleanup(client.CloseIdleConnections)
	if _, err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health() failed: %v", err)
	}
	if !hasDeadline {
		t.Fatal("request context has no deadline; the client must bound every request")
	}
	if need := DefaultServerOptions().WriteTimeout + clientSetupMargin; remaining <= need {
		t.Fatalf("request deadline remaining at start = %s, want more than %s", remaining, need)
	}
}
