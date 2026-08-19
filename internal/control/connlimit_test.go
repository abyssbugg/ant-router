package control

import (
	"net"
	"testing"
	"time"
)

func TestLimitListenerBlocksUntilConnectionCloses(t *testing.T) {
	base := &queuedListener{connections: make(chan net.Conn, 2), closed: make(chan struct{})}
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	base.connections <- firstServer
	base.connections <- secondServer
	listener := newLimitListener(base, 1)
	t.Cleanup(func() {
		_ = listener.Close()
		_ = firstClient.Close()
		_ = secondClient.Close()
	})
	first, err := listener.Accept()
	if err != nil {
		t.Fatalf("first Accept() failed: %v", err)
	}
	secondResult := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			secondResult <- connection
		}
	}()
	select {
	case second := <-secondResult:
		_ = second.Close()
		t.Fatal("second connection bypassed limit")
	case <-time.After(50 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() failed: %v", err)
	}
	select {
	case second := <-secondResult:
		_ = second.Close()
	case <-time.After(time.Second):
		t.Fatal("second Accept() did not resume")
	}
}

type queuedListener struct {
	connections chan net.Conn
	closed      chan struct{}
}

func (l *queuedListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *queuedListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}
func (*queuedListener) Addr() net.Addr { return serverTestAddr("queued") }
