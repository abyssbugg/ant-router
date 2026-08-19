//go:build windows

package controltransport

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLocalEndpointWindows(t *testing.T) {
	options := ResolveOptions{StateRoot: t.TempDir(), UserIdentity: "S-1-5-21-1234"}
	endpoint, err := ResolveLocalEndpoint(DefaultInstance, options)
	if err != nil {
		t.Fatalf("ResolveLocalEndpoint() failed: %v", err)
	}
	if endpoint.Network != "npipe" || !strings.HasPrefix(endpoint.Address, windowsPipePrefix) || strings.Contains(endpoint.Address, options.UserIdentity) {
		t.Errorf("endpoint = %#v", endpoint)
	}
	if !filepath.IsAbs(endpoint.TokenPath) {
		t.Errorf("TokenPath = %q, want absolute", endpoint.TokenPath)
	}
	other, _ := ResolveLocalEndpoint(InstanceName("dev"), options)
	if other.Address == endpoint.Address {
		t.Error("different instances share a pipe")
	}
	otherUser, _ := ResolveLocalEndpoint(DefaultInstance, ResolveOptions{StateRoot: t.TempDir(), UserIdentity: "S-1-5-21-5678"})
	if otherUser.Address == endpoint.Address {
		t.Error("different users share a pipe")
	}
}

func TestWindowsNamedPipeListenDialAndCollision(t *testing.T) {
	endpoint, err := ResolveLocalEndpoint(InstanceName("test-pipe"), ResolveOptions{StateRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("ResolveLocalEndpoint() failed: %v", err)
	}
	listener, err := ListenLocal(endpoint)
	if err != nil {
		t.Fatalf("ListenLocal() failed: %v", err)
	}
	defer listener.Close()
	if _, err := ListenLocal(endpoint); transportCode(err) != CodeAlreadyRunning {
		t.Fatalf("collision code = %q, want %q; error: %v", transportCode(err), CodeAlreadyRunning, err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()
	client, err := (LocalDialer{Endpoint: endpoint}).DialContext(context.Background())
	if err != nil {
		t.Fatalf("DialContext() failed: %v", err)
	}
	server := <-accepted
	defer client.Close()
	defer server.Close()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("client.Write() failed: %v", err)
	}
	buffer := make([]byte, 4)
	if _, err := server.Read(buffer); err != nil || string(buffer) != "ping" {
		t.Fatalf("server.Read() = %q, %v", buffer, err)
	}
}

func TestWindowsNamedPipeDialCancellation(t *testing.T) {
	endpoint, err := ResolveLocalEndpoint(InstanceName("cancel"), ResolveOptions{StateRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("ResolveLocalEndpoint() failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (LocalDialer{Endpoint: endpoint}).DialContext(ctx); err == nil {
		t.Fatal("DialContext() succeeded for cancelled context")
	}
}
