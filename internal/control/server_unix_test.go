//go:build !windows

package control

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
)

func TestServerForcedShutdownWithActiveRequest(t *testing.T) {
	runtimeRoot, err := os.MkdirTemp(os.TempDir(), "ar-force-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	endpoint, err := controltransport.ResolveLocalEndpoint(controltransport.InstanceName("force"), controltransport.ResolveOptions{StateRoot: t.TempDir(), RuntimeRoot: runtimeRoot})
	if err != nil {
		t.Fatalf("ResolveLocalEndpoint() failed: %v", err)
	}
	listener, err := controltransport.ListenLocal(endpoint)
	if err != nil {
		t.Fatalf("ListenLocal() failed: %v", err)
	}
	token, _ := controltoken.Generate(nil)
	providerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseProvider) }) })
	provider := statusProviderFunc(func(context.Context) (RuntimeStatus, error) {
		close(providerStarted)
		<-releaseProvider
		return RuntimeStatus{Instance: "force", DaemonState: "running", StartedAt: time.Now().UTC()}, nil
	})
	server, err := NewServer(listener, token, provider, validServerOptions())
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (controltransport.LocalDialer{Endpoint: endpoint}).DialContext(ctx)
	}}
	client := &http.Client{Transport: transport}
	request, _ := http.NewRequest(http.MethodGet, "http://local/v1/status", nil)
	request.Header.Set("Authorization", "Bearer "+token.String())
	requestDone := make(chan struct{})
	go func() {
		response, _ := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		close(requestDone)
	}()
	<-providerStarted
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.Shutdown(ctx); !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("Shutdown() error = %v, want ErrShutdownTimeout", err)
	}
	releaseOnce.Do(func() { close(releaseProvider) })
	<-requestDone
	if err := server.Wait(); err != nil {
		t.Fatalf("Wait() failed: %v", err)
	}
}

func TestServerUnixAuthenticatedLifecycle(t *testing.T) {
	runtimeRoot, err := os.MkdirTemp(os.TempDir(), "ar-server-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	endpoint, err := controltransport.ResolveLocalEndpoint(controltransport.DefaultInstance, controltransport.ResolveOptions{StateRoot: t.TempDir(), RuntimeRoot: runtimeRoot})
	if err != nil {
		t.Fatalf("ResolveLocalEndpoint() failed: %v", err)
	}
	listener, err := controltransport.ListenLocal(endpoint)
	if err != nil {
		t.Fatalf("ListenLocal() failed: %v", err)
	}
	token, _ := controltoken.Generate(nil)
	server, err := NewServer(listener, token, nil, validServerOptions())
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	<-server.Ready()

	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (controltransport.LocalDialer{Endpoint: endpoint}).DialContext(ctx)
	}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	request, _ := http.NewRequest(http.MethodGet, "http://local/v1/health", nil)
	request.Header.Set("Authorization", "Bearer "+token.String())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("client.Do() failed: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", response.StatusCode)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}
	if err := server.Wait(); err != nil {
		t.Fatalf("Wait() failed: %v", err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() failed: %v", err)
	}
}
