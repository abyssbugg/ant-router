//go:build !windows

package controltransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const maxUnixSocketPath = 100

func resolveLocalEndpoint(instance InstanceName, options ResolveOptions) (Endpoint, error) {
	paths, err := ResolveInstancePaths(options.StateRoot, instance)
	if err != nil {
		return Endpoint{}, err
	}
	runtimeRoot := strings.TrimSpace(options.RuntimeRoot)
	if runtimeRoot == "" {
		runtimeRoot = defaultUnixRuntimeRoot()
	}
	absoluteRoot, err := filepath.Abs(runtimeRoot)
	if err != nil {
		return Endpoint{}, &Error{Code: CodePathResolutionFailed, Path: "runtime_root", Err: errors.New("cannot resolve runtime root")}
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	if err := ensureUnixDirectory(absoluteRoot); err != nil {
		return Endpoint{}, err
	}
	controlRoot := filepath.Join(absoluteRoot, "ant-router")
	if err := ensureUnixDirectory(controlRoot); err != nil {
		return Endpoint{}, err
	}
	address := filepath.Join(controlRoot, instance.String()+".sock")
	if len(address) > maxUnixSocketPath {
		return Endpoint{}, &Error{Code: CodeInvalidEndpoint, Path: "address", Err: errors.New("Unix socket path is too long")}
	}
	return NewEndpoint(instance, "unix", address, paths.TokenPath)
}

func defaultUnixRuntimeRoot() string {
	if runtime.GOOS == "linux" {
		if runtimeRoot := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeRoot != "" {
			return runtimeRoot
		}
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("ant-router-%d", os.Getuid()))
}

func ensureUnixDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return &Error{Code: CodePermissionDenied, Path: "runtime_directory", Err: err}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return &Error{Code: CodePermissionDenied, Path: "runtime_directory", Err: err}
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: CodePermissionDenied, Path: "runtime_directory", Err: errors.New("runtime directory is invalid")}
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return &Error{Code: CodePermissionDenied, Path: "runtime_directory", Err: err}
	}
	return nil
}

func listenLocal(endpoint Endpoint) (*ManagedListener, error) {
	if endpoint.Network != "unix" || strings.TrimSpace(endpoint.Address) == "" {
		return nil, &Error{Code: CodeInvalidEndpoint, Path: "endpoint", Err: errors.New("Unix endpoint is invalid")}
	}
	if err := ensureUnixDirectory(filepath.Dir(endpoint.Address)); err != nil {
		return nil, err
	}
	lock, err := acquireUnixEndpointLock(endpoint.Address + ".lock")
	if err != nil {
		return nil, err
	}
	// The endpoint lock is held through stale detection, removal, binding,
	// and the full listener lifetime; cleanup releases it after socket removal.
	listener, err := listenLocalLocked(endpoint)
	if err != nil {
		_ = lock.release()
		return nil, err
	}
	endpointCleanup := listener.cleanup
	listener.cleanup = func() error {
		return errors.Join(endpointCleanup(), lock.release())
	}
	return listener, nil
}

func listenLocalLocked(endpoint Endpoint) (*ManagedListener, error) {
	if endpoint.Network != "unix" || strings.TrimSpace(endpoint.Address) == "" {
		return nil, &Error{Code: CodeInvalidEndpoint, Path: "endpoint", Err: errors.New("Unix endpoint is invalid")}
	}
	if err := ensureUnixDirectory(filepath.Dir(endpoint.Address)); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(endpoint.Address); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
			return nil, &Error{Code: CodePermissionDenied, Path: "endpoint", Err: errors.New("endpoint path is unsafe")}
		}
		probeContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		connection, dialErr := (&net.Dialer{}).DialContext(probeContext, "unix", endpoint.Address)
		cancel()
		if dialErr == nil {
			_ = connection.Close()
			return nil, &Error{Code: CodeAlreadyRunning, Path: "endpoint", Err: errors.New("endpoint is active")}
		}
		if err := os.Remove(endpoint.Address); err != nil {
			return nil, &Error{Code: CodePermissionDenied, Path: "endpoint", Err: err}
		}
	} else if !os.IsNotExist(err) {
		return nil, &Error{Code: CodePermissionDenied, Path: "endpoint", Err: err}
	}
	address, err := net.ResolveUnixAddr("unix", endpoint.Address)
	if err != nil {
		return nil, &Error{Code: CodeInvalidEndpoint, Path: "address", Err: errors.New("Unix endpoint is invalid")}
	}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return nil, &Error{Code: CodeAlreadyRunning, Path: "endpoint", Err: err}
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(endpoint.Address, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(endpoint.Address)
		return nil, &Error{Code: CodePermissionDenied, Path: "endpoint", Err: err}
	}
	ownedSocket, err := os.Lstat(endpoint.Address)
	if err != nil {
		_ = listener.Close()
		_ = os.Remove(endpoint.Address)
		return nil, &Error{Code: CodePermissionDenied, Path: "endpoint", Err: err}
	}
	cleanup := func() error {
		info, err := os.Lstat(endpoint.Address)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(ownedSocket, info) {
			return &Error{Code: CodePermissionDenied, Path: "endpoint", Err: errors.New("endpoint ownership cannot be proven")}
		}
		return os.Remove(endpoint.Address)
	}
	return newManagedListener(listener, cleanup), nil
}

type unixEndpointLock struct{ file *os.File }

func acquireUnixEndpointLock(path string) (*unixEndpointLock, error) {
	descriptor, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, &Error{Code: CodePermissionDenied, Path: "endpoint_lock", Err: err}
	}
	if err := unix.Fchmod(descriptor, 0o600); err != nil {
		_ = unix.Close(descriptor)
		return nil, &Error{Code: CodePermissionDenied, Path: "endpoint_lock", Err: err}
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(descriptor)
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, &Error{Code: CodeAlreadyRunning, Path: "endpoint_lock", Err: errors.New("endpoint lock is held")}
		}
		return nil, &Error{Code: CodePermissionDenied, Path: "endpoint_lock", Err: err}
	}
	return &unixEndpointLock{file: os.NewFile(uintptr(descriptor), path)}, nil
}

func (l *unixEndpointLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	return errors.Join(unlockErr, closeErr)
}

func dialLocal(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if endpoint.Network != "unix" || strings.TrimSpace(endpoint.Address) == "" {
		return nil, &Error{Code: CodeInvalidEndpoint, Path: "endpoint", Err: errors.New("Unix endpoint is invalid")}
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", endpoint.Address)
	if err != nil {
		return nil, err
	}
	return connection, nil
}
