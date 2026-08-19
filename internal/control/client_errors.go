package control

import (
	"errors"
	"fmt"
)

type ClientErrorKind string

const (
	ClientErrorToken      ClientErrorKind = "token"
	ClientErrorConnection ClientErrorKind = "connection"
	ClientErrorTimeout    ClientErrorKind = "timeout"
	ClientErrorCancelled  ClientErrorKind = "cancelled"
	ClientErrorProtocol   ClientErrorKind = "protocol"
	ClientErrorAPI        ClientErrorKind = "api"
)

type ClientError struct {
	Kind      ClientErrorKind
	Status    int
	API       ErrorCode
	RequestID string
	Err       error
}

func (e *ClientError) Error() string {
	if e == nil {
		return ""
	}
	if e.API != "" {
		return fmt.Sprintf("control client %s error: %s", e.Kind, e.API)
	}
	return fmt.Sprintf("control client %s error", e.Kind)
}

func (e *ClientError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ClientErrorKindOf(err error) (ClientErrorKind, bool) {
	var target *ClientError
	if errors.As(err, &target) && target != nil {
		return target.Kind, true
	}
	return "", false
}
