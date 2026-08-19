package control

import (
	"net"
	"sync"
)

type limitListener struct {
	net.Listener
	semaphore chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func newLimitListener(listener net.Listener, maximum int) net.Listener {
	return &limitListener{Listener: listener, semaphore: make(chan struct{}, maximum), done: make(chan struct{})}
}

func (l *limitListener) Accept() (net.Conn, error) {
	select {
	case l.semaphore <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	connection, err := l.Listener.Accept()
	if err != nil {
		<-l.semaphore
		return nil, err
	}
	return &limitedConnection{Conn: connection, release: func() { <-l.semaphore }}, nil
}

func (l *limitListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.done)
		l.closeErr = l.Listener.Close()
	})
	return l.closeErr
}

type limitedConnection struct {
	net.Conn
	releaseOnce sync.Once
	release     func()
}

func (c *limitedConnection) Close() error {
	err := c.Conn.Close()
	c.releaseOnce.Do(c.release)
	return err
}
