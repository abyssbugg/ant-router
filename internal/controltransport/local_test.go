package controltransport

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestManagedListenerCloseIsIdempotent(t *testing.T) {
	first, second := net.Pipe()
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("second.Close() failed: %v", err)
		}
	})
	cleanupCalls := 0
	listener := newManagedListener(&singleConnectionListener{connection: first}, func() error {
		cleanupCalls++
		return nil
	})
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}
	if cleanupCalls != 1 {
		t.Errorf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestLocalDialerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dialer := LocalDialer{Endpoint: Endpoint{Instance: DefaultInstance, Network: "invalid", Address: "invalid", TokenPath: "token"}}
	if _, err := dialer.DialContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("DialContext() error = %v, want context canceled", err)
	}
}

func transportCode(err error) Code {
	code, _ := CodeOf(err)
	return code
}

type singleConnectionListener struct {
	connection net.Conn
	closed     bool
}

func (l *singleConnectionListener) Accept() (net.Conn, error) {
	if l.closed || l.connection == nil {
		return nil, net.ErrClosed
	}
	connection := l.connection
	l.connection = nil
	return connection, nil
}
func (l *singleConnectionListener) Close() error {
	l.closed = true
	if l.connection != nil {
		return l.connection.Close()
	}
	return nil
}
func (l *singleConnectionListener) Addr() net.Addr { return localTestAddr("local") }

type localTestAddr string

func (a localTestAddr) Network() string { return string(a) }
func (a localTestAddr) String() string  { return string(a) }
