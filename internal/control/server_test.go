package control

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
)

func validServerOptions() ServerOptions {
	return ServerOptions{
		Build:             buildinfo.Info{Version: "test", Commit: "abc", Date: "2026-08-19"},
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ShutdownTimeout:   5 * time.Second,
		MaxHeaderBytes:    8 << 10,
		MaxConnections:    32,
		RequestIDs:        func() (string, error) { return "request-123", nil },
	}
}

func TestNewServerValidation(t *testing.T) {
	listener := &closedListener{}
	verifier := verifierFunc(func(string) bool { return true })
	if _, err := NewServer(nil, verifier, nil, validServerOptions()); err == nil {
		t.Error("NewServer accepted nil listener")
	}
	if _, err := NewServer(listener, nil, nil, validServerOptions()); err == nil {
		t.Error("NewServer accepted nil verifier")
	}
	invalid := validServerOptions()
	invalid.MaxConnections = 0
	if _, err := NewServer(listener, verifier, nil, invalid); err == nil {
		t.Error("NewServer accepted zero connection limit")
	}
}

func TestServerStartOnlyOnceAndWait(t *testing.T) {
	listener := &closedListener{}
	server, err := NewServer(listener, verifierFunc(func(string) bool { return true }), nil, validServerOptions())
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	<-server.Ready()
	if !errors.Is(server.Start(), ErrServerStarted) {
		t.Fatalf("second Start() error = %v, want ErrServerStarted", server.Start())
	}
	if err := server.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil for closed listener", err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}
}

func TestServerOptionsDefaults(t *testing.T) {
	options := DefaultServerOptions()
	if options.ReadHeaderTimeout != 2*time.Second || options.ReadTimeout != 5*time.Second || options.WriteTimeout != 10*time.Second || options.IdleTimeout != 30*time.Second || options.ShutdownTimeout != 5*time.Second || options.MaxHeaderBytes != 8<<10 || options.MaxConnections != 32 {
		t.Errorf("defaults = %#v", options)
	}
}

type closedListener struct{}

func (*closedListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (*closedListener) Close() error              { return nil }
func (*closedListener) Addr() net.Addr            { return serverTestAddr("closed") }

type serverTestAddr string

func (a serverTestAddr) Network() string { return string(a) }
func (a serverTestAddr) String() string  { return string(a) }

func TestServerCancelledCallerStopsWaiting(t *testing.T) {
	listener := &blockingListener{connections: make(chan net.Conn), closed: make(chan struct{})}
	server, err := NewServer(listener, verifierFunc(func(string) bool { return true }), nil, validServerOptions())
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.Shutdown(ctx); !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("Shutdown() error = %v, want ErrShutdownTimeout", err)
	}
	if err := server.Wait(); err != nil {
		t.Fatalf("Wait() failed: %v", err)
	}
}

type blockingListener struct {
	connections chan net.Conn
	closed      chan struct{}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	if l.closed == nil {
		l.closed = make(chan struct{})
	}
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *blockingListener) Close() error {
	if l.closed == nil {
		l.closed = make(chan struct{})
	}
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}
func (*blockingListener) Addr() net.Addr { return serverTestAddr("blocking") }

var _ = http.StatusOK
