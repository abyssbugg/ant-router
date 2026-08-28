package dataplane

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"sync"
	"time"
)

var (
	ErrServerStarted    = errors.New("data-plane server already started")
	ErrServerNotStarted = errors.New("data-plane server not started")
	ErrShutdownTimeout  = errors.New("data-plane server shutdown deadline exceeded")
)

// ServerOptions configures the loopback HTTP server lifecycle.
type ServerOptions struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

// DefaultServerOptions returns bounded defaults suitable for local inference.
func DefaultServerOptions() ServerOptions {
	return ServerOptions{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, ShutdownTimeout: 10 * time.Second, MaxHeaderBytes: 8 << 10}
}

// ListenLoopback binds one literal IPv4 loopback address.
func ListenLoopback(address string) (net.Listener, error) {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil || !endpoint.Addr().Is4() || !endpoint.Addr().IsLoopback() {
		return nil, errors.New("data-plane address must be literal IPv4 loopback")
	}
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), "tcp4", address)
	if err != nil {
		return nil, errors.New("data-plane listen failed")
	}
	return listener, nil
}

// Server owns one loopback HTTP listener and graceful lifecycle.
type Server struct {
	mu              sync.Mutex
	listener        net.Listener
	httpServer      *http.Server
	ready           chan struct{}
	done            chan struct{}
	started         bool
	serveErr        error
	shutdownOnce    sync.Once
	shutdownDone    chan struct{}
	shutdownTimeout time.Duration
	shutdownErr     error
}

// NewServer constructs a server around an already-bound loopback listener.
func NewServer(listener net.Listener, handler http.Handler, options ServerOptions) (*Server, error) {
	if nilInterfaceValue(listener) || nilInterfaceValue(handler) {
		return nil, errors.New("data-plane listener and handler are required")
	}
	if options.ReadHeaderTimeout <= 0 || options.ReadTimeout <= 0 || options.IdleTimeout <= 0 || options.ShutdownTimeout <= 0 || options.MaxHeaderBytes <= 0 {
		return nil, errors.New("data-plane server options are invalid")
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.IP.To4() == nil || !address.IP.IsLoopback() {
		return nil, errors.New("data-plane listener must be IPv4 loopback")
	}
	return &Server{
		listener:   listener,
		httpServer: &http.Server{Handler: handler, ReadHeaderTimeout: options.ReadHeaderTimeout, ReadTimeout: options.ReadTimeout, IdleTimeout: options.IdleTimeout, MaxHeaderBytes: options.MaxHeaderBytes},
		ready:      make(chan struct{}), done: make(chan struct{}), shutdownDone: make(chan struct{}), shutdownTimeout: options.ShutdownTimeout,
	}, nil
}

// Start begins serving exactly once.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return ErrServerStarted
	}
	s.started = true
	go func() {
		close(s.ready)
		err := s.httpServer.Serve(s.listener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		}
		s.mu.Lock()
		s.serveErr = err
		s.mu.Unlock()
		close(s.done)
	}()
	return nil
}

// Ready closes when the serving goroutine has started.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Wait blocks until serving stops.
func (s *Server) Wait() error {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return ErrServerNotStarted
	}
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serveErr
}

// Shutdown drains active requests once, then force-closes after the configured deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return nil
	}
	s.shutdownOnce.Do(func() { go s.runShutdown() })
	select {
	case <-s.shutdownDone:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.shutdownErr
	case <-ctx.Done():
		return errors.Join(ErrShutdownTimeout, ctx.Err())
	}
}

func (s *Server) runShutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	err := s.httpServer.Shutdown(ctx)
	if err != nil {
		err = errors.Join(ErrShutdownTimeout, err, s.httpServer.Close())
	}
	<-s.done
	s.mu.Lock()
	s.shutdownErr = err
	s.mu.Unlock()
	close(s.shutdownDone)
}

func nilInterfaceValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
