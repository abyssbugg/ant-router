//go:build !windows

package controltransport

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func unixTestEndpoint(t *testing.T, instance InstanceName) Endpoint {
	t.Helper()
	stateRoot := t.TempDir()
	runtimeRoot, err := os.MkdirTemp(os.TempDir(), "ar-")
	if err != nil {
		t.Fatalf("os.MkdirTemp(runtime) failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	endpoint, err := ResolveLocalEndpoint(instance, ResolveOptions{StateRoot: stateRoot, RuntimeRoot: runtimeRoot})
	if err != nil {
		t.Fatalf("ResolveLocalEndpoint() failed: %v", err)
	}
	return endpoint
}

func TestResolveLocalEndpointUnix(t *testing.T) {
	endpoint := unixTestEndpoint(t, DefaultInstance)
	if endpoint.Network != "unix" || !filepath.IsAbs(endpoint.Address) || !strings.HasSuffix(endpoint.Address, "default.sock") {
		t.Errorf("endpoint = %#v", endpoint)
	}
	if !filepath.IsAbs(endpoint.TokenPath) {
		t.Errorf("TokenPath = %q, want absolute", endpoint.TokenPath)
	}
}

func TestUnixListenDialPermissionsAndCleanup(t *testing.T) {
	endpoint := unixTestEndpoint(t, DefaultInstance)
	listener, err := ListenLocal(endpoint)
	if err != nil {
		t.Fatalf("ListenLocal() failed: %v", err)
	}
	info, err := os.Lstat(endpoint.Address)
	if err != nil {
		t.Fatalf("os.Lstat(socket) failed: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v", info.Mode())
	}
	runtimeInfo, err := os.Stat(filepath.Dir(endpoint.Address))
	if err != nil {
		t.Fatalf("os.Stat(runtime) failed: %v", err)
	}
	if runtimeInfo.Mode().Perm() != 0o700 {
		t.Errorf("runtime mode = %04o, want 0700", runtimeInfo.Mode().Perm())
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
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	if _, err := os.Lstat(endpoint.Address); !os.IsNotExist(err) {
		t.Errorf("socket remains after Close: %v", err)
	}
}

func TestUnixLiveAndStaleSocketHandling(t *testing.T) {
	endpoint := unixTestEndpoint(t, DefaultInstance)
	listener, err := ListenLocal(endpoint)
	if err != nil {
		t.Fatalf("ListenLocal() failed: %v", err)
	}
	defer listener.Close()
	if _, err := ListenLocal(endpoint); transportCode(err) != CodeAlreadyRunning {
		t.Fatalf("live collision code = %q, want %q; error: %v", transportCode(err), CodeAlreadyRunning, err)
	}

	stale := unixTestEndpoint(t, InstanceName("stale"))
	if err := os.MkdirAll(filepath.Dir(stale.Address), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	unixAddress, _ := net.ResolveUnixAddr("unix", stale.Address)
	old, err := net.ListenUnix("unix", unixAddress)
	if err != nil {
		t.Fatalf("net.ListenUnix(stale) failed: %v", err)
	}
	old.SetUnlinkOnClose(false)
	_ = old.Close()
	fresh, err := ListenLocal(stale)
	if err != nil {
		t.Fatalf("ListenLocal(stale) failed: %v", err)
	}
	_ = fresh.Close()
}

func TestUnixEndpointIsolationAndInvalidPaths(t *testing.T) {
	first := unixTestEndpoint(t, DefaultInstance)
	second := unixTestEndpoint(t, InstanceName("dev"))
	if first.Address == second.Address {
		t.Error("different instances share a socket")
	}
	tooLongRoot := filepath.Join(t.TempDir(), strings.Repeat("x", 120))
	_, err := ResolveLocalEndpoint(DefaultInstance, ResolveOptions{StateRoot: t.TempDir(), RuntimeRoot: tooLongRoot})
	if transportCode(err) != CodeInvalidEndpoint {
		t.Fatalf("long endpoint code = %q, want %q", transportCode(err), CodeInvalidEndpoint)
	}
}

func TestUnixRuntimeAndSocketSymlinksRejected(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	runtimeLink := filepath.Join(root, "runtime-link")
	if err := os.Symlink(target, runtimeLink); err != nil {
		t.Fatalf("os.Symlink(runtime) failed: %v", err)
	}
	_, err := ResolveLocalEndpoint(DefaultInstance, ResolveOptions{StateRoot: t.TempDir(), RuntimeRoot: runtimeLink})
	if transportCode(err) != CodePermissionDenied {
		t.Fatalf("runtime symlink code = %q, want %q", transportCode(err), CodePermissionDenied)
	}

	endpoint := unixTestEndpoint(t, InstanceName("link"))
	if err := os.MkdirAll(filepath.Dir(endpoint.Address), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	fileTarget := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(fileTarget, []byte("target"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	if err := os.Symlink(fileTarget, endpoint.Address); err != nil {
		t.Fatalf("os.Symlink(socket) failed: %v", err)
	}
	if _, err := ListenLocal(endpoint); transportCode(err) != CodePermissionDenied {
		t.Fatalf("socket symlink code = %q, want %q", transportCode(err), CodePermissionDenied)
	}
}

func TestUnixCloseDoesNotRemoveReplacementSocket(t *testing.T) {
	endpoint := unixTestEndpoint(t, InstanceName("replacement"))
	listener, err := ListenLocal(endpoint)
	if err != nil {
		t.Fatalf("ListenLocal() failed: %v", err)
	}
	if err := os.Remove(endpoint.Address); err != nil {
		t.Fatalf("os.Remove(original socket) failed: %v", err)
	}
	address, _ := net.ResolveUnixAddr("unix", endpoint.Address)
	replacement, err := net.ListenUnix("unix", address)
	if err != nil {
		t.Fatalf("net.ListenUnix(replacement) failed: %v", err)
	}
	replacement.SetUnlinkOnClose(false)
	t.Cleanup(func() {
		_ = replacement.Close()
		_ = os.Remove(endpoint.Address)
	})
	if err := listener.Close(); transportCode(err) != CodePermissionDenied {
		t.Fatalf("Close() code = %q, want %q; error: %v", transportCode(err), CodePermissionDenied, err)
	}
	if info, err := os.Lstat(endpoint.Address); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("replacement socket was removed or changed: %v, %v", info, err)
	}
}

func TestUnixConcurrentStaleSocketReclamation(t *testing.T) {
	endpoint := unixTestEndpoint(t, InstanceName("race"))
	if err := os.MkdirAll(filepath.Dir(endpoint.Address), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	address, _ := net.ResolveUnixAddr("unix", endpoint.Address)
	stale, err := net.ListenUnix("unix", address)
	if err != nil {
		t.Fatalf("net.ListenUnix(stale) failed: %v", err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()

	type result struct {
		listener *ManagedListener
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			listener, listenErr := ListenLocal(endpoint)
			results <- result{listener: listener, err: listenErr}
		}()
	}
	close(start)
	first, second := <-results, <-results
	successes, collisions := 0, 0
	for _, outcome := range []result{first, second} {
		if outcome.err == nil {
			successes++
			t.Cleanup(func() { _ = outcome.listener.Close() })
		} else if transportCode(outcome.err) == CodeAlreadyRunning {
			collisions++
		}
	}
	if successes != 1 || collisions != 1 {
		t.Fatalf("concurrent starts: successes=%d collisions=%d errors=%v/%v", successes, collisions, first.err, second.err)
	}
}

func TestUnixDialDeadline(t *testing.T) {
	endpoint := unixTestEndpoint(t, DefaultInstance)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if _, err := (LocalDialer{Endpoint: endpoint}).DialContext(ctx); err == nil {
		t.Fatal("DialContext() succeeded after deadline")
	}
}
