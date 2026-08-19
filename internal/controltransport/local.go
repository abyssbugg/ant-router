package controltransport

import (
	"context"
	"errors"
	"net"
	"sync"
)

type ResolveOptions struct {
	StateRoot    string
	RuntimeRoot  string
	UserIdentity string
}

type ManagedListener struct {
	net.Listener
	cleanup   func() error
	closeOnce sync.Once
	closeErr  error
}

func newManagedListener(listener net.Listener, cleanup func() error) *ManagedListener {
	if cleanup == nil {
		cleanup = func() error { return nil }
	}
	return &ManagedListener{Listener: listener, cleanup: cleanup}
}

func (l *ManagedListener) Close() error {
	l.closeOnce.Do(func() {
		l.closeErr = errors.Join(l.Listener.Close(), l.cleanup())
	})
	return l.closeErr
}

type LocalDialer struct {
	Endpoint Endpoint
}

func (d LocalDialer) DialContext(ctx context.Context) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return dialLocal(ctx, d.Endpoint)
}

func ResolveLocalEndpoint(instance InstanceName, options ResolveOptions) (Endpoint, error) {
	return resolveLocalEndpoint(instance, options)
}

func ListenLocal(endpoint Endpoint) (*ManagedListener, error) {
	return listenLocal(endpoint)
}
