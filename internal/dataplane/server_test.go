package dataplane

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestListenLoopbackRejectsNonLiteralOrNonLoopback(t *testing.T) {
	for _, address := range []string{"localhost:8787", "0.0.0.0:8787", "192.0.2.1:8787", "[::1]:8787", "bad"} {
		if listener, err := ListenLoopback(address); err == nil {
			_ = listener.Close()
			t.Fatalf("address %q accepted", address)
		}
	}
	listener, err := ListenLoopback("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if host, _, _ := net.SplitHostPort(listener.Addr().String()); host != "127.0.0.1" {
		t.Fatalf("listener address = %q", listener.Addr())
	}
}

func TestServerLifecycle(t *testing.T) {
	listener, err := ListenLoopback("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), DefaultServerOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	<-server.Ready()
	if !errors.Is(server.Start(), ErrServerStarted) {
		t.Fatalf("second Start = %v", server.Start())
	}
	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown = %v", err)
	}
}

func TestNewServerValidationAndDefaults(t *testing.T) {
	options := DefaultServerOptions()
	if options.ReadHeaderTimeout <= 0 || options.IdleTimeout <= 0 || options.ShutdownTimeout <= 0 || options.MaxHeaderBytes <= 0 {
		t.Fatalf("defaults = %#v", options)
	}
	listener := &closedListener{}
	if _, err := NewServer(nil, http.NotFoundHandler(), options); err == nil {
		t.Fatal("nil listener accepted")
	}
	if _, err := NewServer(listener, nil, options); err == nil {
		t.Fatal("nil handler accepted")
	}
	invalid := options
	invalid.MaxHeaderBytes = 0
	if _, err := NewServer(listener, http.NotFoundHandler(), invalid); err == nil {
		t.Fatal("invalid options accepted")
	}
}

type closedListener struct{}

func (*closedListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (*closedListener) Close() error              { return nil }
func (*closedListener) Addr() net.Addr            { return testAddress("closed") }

type testAddress string

func (a testAddress) Network() string { return string(a) }
func (a testAddress) String() string  { return string(a) }
