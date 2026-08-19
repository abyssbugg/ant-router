//go:build controlstress && !windows

package control

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
)

func TestAuthenticatedServerStress(t *testing.T) {
	testContext, cancelTest := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelTest()
	runtimeRoot, err := os.MkdirTemp(os.TempDir(), "ar-stress-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeRoot) })
	endpoint, err := controltransport.ResolveLocalEndpoint(controltransport.InstanceName("stress"), controltransport.ResolveOptions{StateRoot: t.TempDir(), RuntimeRoot: runtimeRoot})
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
	client, err := NewClient(controltransport.LocalDialer{Endpoint: endpoint}, tokenSourceFunc(func(context.Context) (controltoken.Token, error) { return token, nil }), DefaultClientOptions())
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer client.CloseIdleConnections()

	const workers = 64
	const requestsPerWorker = 20
	errorsFound := make(chan error, 1)
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			for range requestsPerWorker {
				response, requestErr := client.Health(testContext)
				if requestErr != nil || response.Status != "ok" {
					select {
					case errorsFound <- fmt.Errorf("health failed: %#v, %v", response, requestErr):
					default:
					}
					return
				}
			}
		}()
	}
	wait.Wait()
	select {
	case requestErr := <-errorsFound:
		t.Fatal(requestErr)
	default:
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}
	if err := server.Wait(); err != nil {
		t.Fatalf("Wait() failed: %v", err)
	}
}
