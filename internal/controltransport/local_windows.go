//go:build windows

package controltransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const windowsPipePrefix = `\\.\pipe\ant-router-`

func resolveLocalEndpoint(instance InstanceName, options ResolveOptions) (Endpoint, error) {
	paths, err := ResolveInstancePaths(options.StateRoot, instance)
	if err != nil {
		return Endpoint{}, err
	}
	identity := strings.TrimSpace(options.UserIdentity)
	if identity == "" {
		identity, err = currentWindowsSID()
		if err != nil {
			return Endpoint{}, &Error{Code: CodePermissionDenied, Path: "user_identity", Err: errors.New("cannot resolve Windows user identity")}
		}
	}
	digest := sha256.Sum256([]byte(identity))
	userHash := hex.EncodeToString(digest[:8])
	address := windowsPipePrefix + userHash + "-" + instance.String()
	return NewEndpoint(instance, "npipe", address, paths.TokenPath)
}

func listenLocal(endpoint Endpoint) (*ManagedListener, error) {
	if endpoint.Network != "npipe" || !strings.HasPrefix(endpoint.Address, windowsPipePrefix) {
		return nil, &Error{Code: CodeInvalidEndpoint, Path: "endpoint", Err: errors.New("named-pipe endpoint is invalid")}
	}
	sid, err := currentWindowsSID()
	if err != nil {
		return nil, &Error{Code: CodePermissionDenied, Path: "user_identity", Err: errors.New("cannot resolve Windows user identity")}
	}
	securityDescriptor := fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)", sid)
	listener, err := winio.ListenPipe(endpoint.Address, &winio.PipeConfig{
		SecurityDescriptor: securityDescriptor,
		MessageMode:        false,
		InputBufferSize:    64 << 10,
		OutputBufferSize:   64 << 10,
	})
	if err != nil {
		probeContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		connection, probeErr := winio.DialPipeContext(probeContext, endpoint.Address)
		if probeErr == nil {
			_ = connection.Close()
			return nil, &Error{Code: CodeAlreadyRunning, Path: "endpoint", Err: errors.New("named-pipe endpoint is active")}
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, &Error{Code: CodePermissionDenied, Path: "endpoint", Err: errors.New("named-pipe endpoint permission denied")}
		}
		return nil, &Error{Code: CodeInvalidEndpoint, Path: "endpoint", Err: errors.New("named-pipe endpoint is unavailable")}
	}
	return newManagedListener(listener, nil), nil
}

func dialLocal(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if endpoint.Network != "npipe" || !strings.HasPrefix(endpoint.Address, windowsPipePrefix) {
		return nil, &Error{Code: CodeInvalidEndpoint, Path: "endpoint", Err: errors.New("named-pipe endpoint is invalid")}
	}
	connection, err := winio.DialPipeContext(ctx, endpoint.Address)
	if err != nil {
		return nil, err
	}
	return connection, nil
}

func currentWindowsSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}
