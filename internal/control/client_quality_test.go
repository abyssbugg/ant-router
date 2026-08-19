package control

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/controltoken"
)

func TestClientTimeoutIncludesTokenSource(t *testing.T) {
	options := DefaultClientOptions()
	options.RequestTimeout = 10 * time.Millisecond
	client, err := NewClient(clientDialerFunc(func(context.Context) (net.Conn, error) {
		t.Error("dialer was called")
		return nil, context.Canceled
	}), tokenSourceFunc(func(ctx context.Context) (controltoken.Token, error) {
		<-ctx.Done()
		return controltoken.Token{}, ctx.Err()
	}), options)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	if _, err := client.Health(context.Background()); ClientErrorKindValue(err) != ClientErrorTimeout {
		t.Fatalf("kind = %q, want %q; error: %v", ClientErrorKindValue(err), ClientErrorTimeout, err)
	}
}

func TestClientCallerCancellationIsDistinct(t *testing.T) {
	token, _ := controltoken.Generate(nil)
	client, err := NewClient(clientDialerFunc(func(context.Context) (net.Conn, error) {
		return nil, context.Canceled
	}), tokenSourceFunc(func(context.Context) (controltoken.Token, error) { return token, nil }), DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Health(ctx); ClientErrorKindValue(err) != ClientErrorCancelled {
		t.Fatalf("kind = %q, want %q; error: %v", ClientErrorKindValue(err), ClientErrorCancelled, err)
	}
}

func TestClientRejectsAPIVersionMismatch(t *testing.T) {
	client, _, _ := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = WriteJSON(writer, http.StatusOK, HealthResponse{Status: "ok", APIVersion: "v2"})
	}))
	if _, err := client.Health(context.Background()); ClientErrorKindValue(err) != ClientErrorProtocol {
		t.Fatalf("kind = %q, want %q", ClientErrorKindValue(err), ClientErrorProtocol)
	}
}

func TestClientRejectsUnknownAPIErrorAndUsesHeaderRequestID(t *testing.T) {
	unknown, _, _ := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = WriteJSON(writer, http.StatusBadRequest, ErrorResponse{Error: ErrorDetail{Code: ErrorCode("future_error"), Message: "future", RequestID: "request-body"}})
	}))
	if _, err := unknown.Health(context.Background()); ClientErrorKindValue(err) != ClientErrorProtocol {
		t.Fatalf("unknown API code kind = %q, want %q", ClientErrorKindValue(err), ClientErrorProtocol)
	}

	fallback, _, _ := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set(RequestIDHeader, "request-header")
		_ = WriteJSON(writer, http.StatusUnauthorized, ErrorResponse{Error: ErrorDetail{Code: CodeUnauthorized, Message: "authorization failed"}})
	}))
	_, err := fallback.Health(context.Background())
	var clientErr *ClientError
	if !errors.As(err, &clientErr) || clientErr.RequestID != "request-header" {
		t.Fatalf("request ID fallback = %#v, error: %v", clientErr, err)
	}
}
