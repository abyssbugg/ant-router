package control

import "testing"

// TestDefaultClientTimeoutCoversServerRequestBudget guards against the client
// abandoning requests the server is still allowed to finish. The server gives
// a handler ReadTimeout to complete (RequestTimeout middleware) and may spend
// up to WriteTimeout writing the response; durable config writes fsync on the
// request path and can be slow on loaded hosts. A shorter client budget turns
// a slow-but-successful mutation into a spurious client failure.
func TestDefaultClientTimeoutCoversServerRequestBudget(t *testing.T) {
	client := DefaultClientOptions()
	server := DefaultServerOptions()
	if client.RequestTimeout <= server.ReadTimeout {
		t.Fatalf("client RequestTimeout %s must exceed server request budget %s", client.RequestTimeout, server.ReadTimeout)
	}
	if client.RequestTimeout < server.WriteTimeout {
		t.Fatalf("client RequestTimeout %s must cover server WriteTimeout %s", client.RequestTimeout, server.WriteTimeout)
	}
}
