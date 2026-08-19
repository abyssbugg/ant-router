package control

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/controltoken"
)

type clientDialerFunc func(context.Context) (net.Conn, error)

func (f clientDialerFunc) DialContext(ctx context.Context) (net.Conn, error) { return f(ctx) }

type tokenSourceFunc func(context.Context) (controltoken.Token, error)

func (f tokenSourceFunc) Token(ctx context.Context) (controltoken.Token, error) { return f(ctx) }

type handlerDialer struct {
	handler     http.Handler
	mu          sync.Mutex
	dials       int
	connections []net.Conn
	wait        sync.WaitGroup
}

func (d *handlerDialer) DialContext(ctx context.Context) (net.Conn, error) {
	serverConnection, clientConnection := net.Pipe()
	d.mu.Lock()
	d.dials++
	d.connections = append(d.connections, serverConnection)
	d.wait.Add(1)
	d.mu.Unlock()
	go func() {
		defer d.wait.Done()
		_ = http.Serve(&clientSingleConnectionListener{connection: serverConnection}, d.handler)
	}()
	return clientConnection, nil
}

func (d *handlerDialer) DialCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dials
}

func (d *handlerDialer) Close() {
	d.mu.Lock()
	connections := append([]net.Conn(nil), d.connections...)
	d.mu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
	d.wait.Wait()
}

func testClient(t *testing.T, handler http.Handler) (*Client, *handlerDialer, controltoken.Token) {
	t.Helper()
	token, err := controltoken.Generate(bytes.NewReader(bytes.Repeat([]byte{0x44}, controltoken.TokenSize)))
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}
	dialer := &handlerDialer{handler: handler}
	client, err := NewClient(dialer, tokenSourceFunc(func(context.Context) (controltoken.Token, error) { return token, nil }), DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	t.Cleanup(func() {
		client.CloseIdleConnections()
		dialer.Close()
	})
	return client, dialer, token
}

func TestClientHealthAndAuthentication(t *testing.T) {
	var expectedToken string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+expectedToken || request.Header.Get("Accept") != "application/json" {
			t.Errorf("request headers = %#v", request.Header)
		}
		_ = WriteJSON(writer, http.StatusOK, HealthResponse{Status: "ok", APIVersion: APIVersion})
	})
	client, _, token := testClient(t, handler)
	expectedToken = token.String()
	response, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() failed: %v", err)
	}
	if response.Status != "ok" || response.APIVersion != APIVersion {
		t.Errorf("response = %#v", response)
	}
}

func TestClientVersionStatusAndKeepAlive(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/health":
			_ = WriteJSON(writer, http.StatusOK, HealthResponse{Status: "ok", APIVersion: APIVersion})
		case "/v1/version":
			_ = WriteJSON(writer, http.StatusOK, VersionResponse{Name: "ant-router", Version: "1", APIVersion: APIVersion})
		case "/v1/status":
			_ = WriteJSON(writer, http.StatusOK, StatusResponse{Instance: "default", DaemonState: "running", StartedAt: time.Now().UTC()})
		default:
			_ = WriteError(writer, http.StatusNotFound, CodeNotFound, "request-123")
		}
	})
	client, dialer, _ := testClient(t, handler)
	if _, err := client.Version(context.Background()); err != nil {
		t.Fatalf("Version() failed: %v", err)
	}
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if dialer.DialCount() != 1 {
		t.Errorf("dial count = %d, want 1 keep-alive connection", dialer.DialCount())
	}
	client.CloseIdleConnections()
	if _, err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health after close idle failed: %v", err)
	}
	if dialer.DialCount() != 2 {
		t.Errorf("dial count after CloseIdleConnections = %d, want 2", dialer.DialCount())
	}
}

func TestClientAPIError(t *testing.T) {
	client, _, _ := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = WriteError(writer, http.StatusUnauthorized, CodeUnauthorized, "request-abc")
	}))
	_, err := client.Health(context.Background())
	var clientErr *ClientError
	if !errors.As(err, &clientErr) || clientErr.Kind != ClientErrorAPI || clientErr.API != CodeUnauthorized || clientErr.Status != http.StatusUnauthorized || clientErr.RequestID != "request-abc" {
		t.Fatalf("client error = %#v, %v", clientErr, err)
	}
}

func TestClientProtocolFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
	}{
		{name: "content type", handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("not json"))
		})},
		{name: "unknown field", handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", JSONContentType)
			_, _ = writer.Write([]byte(`{"status":"ok","api_version":"v1","extra":true}`))
		})},
		{name: "trailing JSON", handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", JSONContentType)
			_, _ = writer.Write([]byte(`{"status":"ok","api_version":"v1"} {"extra":true}`))
		})},
		{name: "oversized", handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", JSONContentType)
			_, _ = writer.Write([]byte(strings.Repeat("x", int(MaxResponseBody)+1)))
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, _, _ := testClient(t, test.handler)
			_, err := client.Health(context.Background())
			if kind, ok := ClientErrorKindOf(err); !ok || kind != ClientErrorProtocol {
				t.Fatalf("kind = %q, present = %v, want %q; error: %v", kind, ok, ClientErrorProtocol, err)
			}
		})
	}
}

func TestClientTokenConnectionAndTimeoutErrors(t *testing.T) {
	sentinel := errors.New("token unavailable")
	client, err := NewClient(clientDialerFunc(func(context.Context) (net.Conn, error) { return nil, errors.New("should not dial") }), tokenSourceFunc(func(context.Context) (controltoken.Token, error) { return controltoken.Token{}, sentinel }), DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	if _, err := client.Health(context.Background()); ClientErrorKindValue(err) != ClientErrorToken {
		t.Fatalf("token error kind = %q", ClientErrorKindValue(err))
	}

	token, _ := controltoken.Generate(nil)
	client, _ = NewClient(clientDialerFunc(func(context.Context) (net.Conn, error) { return nil, errors.New("dial failed") }), tokenSourceFunc(func(context.Context) (controltoken.Token, error) { return token, nil }), DefaultClientOptions())
	if _, err := client.Health(context.Background()); ClientErrorKindValue(err) != ClientErrorConnection {
		t.Fatalf("connection error kind = %q", ClientErrorKindValue(err))
	}

	blocking := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { <-request.Context().Done() })
	options := DefaultClientOptions()
	options.RequestTimeout = 10 * time.Millisecond
	client, _, token = testClient(t, blocking)
	client.requestTimeout = options.RequestTimeout
	if _, err := client.Health(context.Background()); ClientErrorKindValue(err) != ClientErrorTimeout {
		t.Fatalf("timeout error kind = %q; error: %v", ClientErrorKindValue(err), err)
	}
}

type clientSingleConnectionListener struct {
	connection net.Conn
	closed     bool
}

func (l *clientSingleConnectionListener) Accept() (net.Conn, error) {
	if l.closed || l.connection == nil {
		return nil, net.ErrClosed
	}
	connection := l.connection
	l.connection = nil
	return connection, nil
}
func (l *clientSingleConnectionListener) Close() error {
	l.closed = true
	if l.connection != nil {
		return l.connection.Close()
	}
	return nil
}
func (*clientSingleConnectionListener) Addr() net.Addr { return serverTestAddr("client-pipe") }

func ClientErrorKindValue(err error) ClientErrorKind {
	kind, _ := ClientErrorKindOf(err)
	return kind
}
