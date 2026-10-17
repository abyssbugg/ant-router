package main

import (
	"context"
	"errors"

	"github.com/abyssbugg/ant-router/internal/codexintegration"
	"github.com/abyssbugg/ant-router/internal/control"
	"github.com/abyssbugg/ant-router/internal/controltoken"
	"github.com/abyssbugg/ant-router/internal/controltransport"
	"github.com/abyssbugg/ant-router/internal/inferencetoken"
)

var (
	errConnection           = errors.New("daemon connection failed")
	errDataPlaneUnavailable = errors.New("data plane is unavailable")
)

const (
	ExitSuccess        = 0
	ExitUsage          = 2
	ExitConnection     = 3
	ExitAuthentication = 4
	ExitValidation     = 5
	ExitConflict       = 6
	ExitUnavailable    = 7
	ExitInternal       = 8
)

func exitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if errors.Is(err, errUsage) {
		return ExitUsage
	}
	var ctErr *controltransport.Error
	if errors.As(err, &ctErr) && ctErr != nil && ctErr.Code == controltransport.CodeInvalidInstance {
		return ExitUsage
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errConnection) {
		return ExitConnection
	}
	if errors.Is(err, errDataPlaneUnavailable) || errors.Is(err, codexintegration.ErrUnavailable) {
		return ExitUnavailable
	}
	if errors.Is(err, codexintegration.ErrConflict) {
		return ExitConflict
	}
	if errors.Is(err, codexintegration.ErrInvalid) {
		return ExitValidation
	}
	if code, ok := inferencetoken.CodeOf(err); ok {
		switch code {
		case inferencetoken.CodeTokenNotFound, inferencetoken.CodeInvalidToken, inferencetoken.CodeTokenCorrupt:
			return ExitAuthentication
		case inferencetoken.CodePermissionDenied, inferencetoken.CodeStoreUnavailable:
			return ExitConnection
		case inferencetoken.CodeDurabilityUncertain:
			return ExitInternal
		}
	}
	if errors.Is(err, errConfigurationTooLarge) || errors.Is(err, errConfigurationRead) {
		return ExitValidation
	}
	if code, ok := controltoken.CodeOf(err); ok {
		switch code {
		case controltoken.CodeTokenNotFound, controltoken.CodeInvalidToken, controltoken.CodeTokenCorrupt:
			return ExitAuthentication
		case controltoken.CodePermissionDenied, controltoken.CodeStoreUnavailable:
			return ExitConnection
		case controltoken.CodeDurabilityUncertain:
			return ExitInternal
		}
	}
	if code, ok := serviceErrorExit(err); ok {
		return code
	}
	var clientErr *control.ClientError
	if errors.As(err, &clientErr) && clientErr != nil {
		switch clientErr.Kind {
		case control.ClientErrorConnection, control.ClientErrorTimeout, control.ClientErrorCancelled:
			return ExitConnection
		case control.ClientErrorToken:
			return ExitAuthentication
		case control.ClientErrorAPI:
			switch clientErr.API {
			case control.CodeUnauthorized:
				return ExitAuthentication
			case control.CodeConfigurationInvalid, control.CodeConfigurationTooLarge:
				return ExitValidation
			case control.CodeRevisionMismatch, control.CodeGenerationConflict:
				return ExitConflict
			case control.CodeNoConfiguration, control.CodeNoPreviousRevision, control.CodeConfigurationUnavailable:
				return ExitUnavailable
			}
		}
	}
	return ExitInternal
}
