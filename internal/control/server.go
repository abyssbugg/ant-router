package control

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/abyssbugg/ant-router/internal/buildinfo"
)

var (
	ErrServerStarted    = errors.New("control server already started")
	ErrServerNotStarted = errors.New("control server not started")
	ErrShutdownTimeout  = errors.New("control server shutdown deadline exceeded")
)

type ServerOptions struct {
	Build             buildinfo.Info
	Config            ConfigService
	CodexCatalog      CodexCatalogProvider
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	MaxConnections    int
	RequestIDs        RequestIDGenerator
}

func DefaultServerOptions() ServerOptions {
	return ServerOptions{
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ShutdownTimeout:   5 * time.Second,
		MaxHeaderBytes:    8 << 10,
		MaxConnections:    32,
	}
}

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

func NewServer(listener net.Listener, verifier TokenVerifier, status StatusProvider, options ServerOptions) (*Server, error) {
	if listener == nil {
		return nil, errors.New("control listener is required")
	}
	if verifier == nil {
		return nil, errors.New("control token verifier is required")
	}
	if options.ReadHeaderTimeout <= 0 || options.ReadTimeout <= 0 || options.WriteTimeout <= 0 || options.IdleTimeout <= 0 || options.ShutdownTimeout <= 0 || options.MaxHeaderBytes <= 0 || options.MaxConnections <= 0 {
		return nil, errors.New("control server options are invalid")
	}
	router, err := NewRouter(RouterOptions{Build: options.Build, Status: status, Config: options.Config, CodexCatalog: options.CodexCatalog})
	if err != nil {
		return nil, err
	}
	handler := Chain(router,
		RecoverPanics,
		SecurityHeaders,
		RequestIDs(options.RequestIDs),
		Authenticate(verifier),
		RequestTimeout(options.ReadTimeout),
		RequestPolicy,
	)
	limited := newLimitListener(listener, options.MaxConnections)
	return &Server{
		listener: limited,
		httpServer: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: options.ReadHeaderTimeout,
			ReadTimeout:       options.ReadTimeout,
			WriteTimeout:      options.WriteTimeout,
			IdleTimeout:       options.IdleTimeout,
			MaxHeaderBytes:    options.MaxHeaderBytes,
		},
		ready:           make(chan struct{}),
		done:            make(chan struct{}),
		shutdownDone:    make(chan struct{}),
		shutdownTimeout: options.ShutdownTimeout,
	}, nil
}

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

func (s *Server) Ready() <-chan struct{} { return s.ready }

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

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		return nil
	}
	s.shutdownOnce.Do(func() {
		go s.runShutdown(ctx)
	})
	select {
	case <-s.shutdownDone:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.shutdownErr
	default:
	}
	select {
	case <-s.shutdownDone:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.shutdownErr
	case <-ctx.Done():
		return errors.Join(ErrShutdownTimeout, ctx.Err())
	}
}

func (s *Server) runShutdown(parent context.Context) {
	shutdownContext, cancel := context.WithTimeout(parent, s.shutdownTimeout)
	defer cancel()
	shutdownErr := s.httpServer.Shutdown(shutdownContext)
	if shutdownErr != nil {
		closeErr := s.httpServer.Close()
		shutdownErr = errors.Join(ErrShutdownTimeout, shutdownErr, closeErr)
	}
	<-s.done
	s.mu.Lock()
	s.shutdownErr = shutdownErr
	s.mu.Unlock()
	close(s.shutdownDone)
}
