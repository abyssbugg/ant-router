package configservice

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeConfigurationInvalid     Code = "configuration_invalid"
	CodeConfigurationTooLarge    Code = "configuration_too_large"
	CodeRevisionMismatch         Code = "revision_mismatch"
	CodeGenerationConflict       Code = "generation_conflict"
	CodeNoConfiguration          Code = "no_configuration"
	CodeNoPreviousRevision       Code = "no_previous_revision"
	CodeConfigurationUnavailable Code = "configuration_unavailable"
	CodeCancelled                Code = "cancelled"
	CodeRuntimeNotUpdated        Code = "runtime_not_updated"
)

type Error struct {
	Code Code
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func CodeOf(err error) (Code, bool) {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target.Code, true
	}
	return "", false
}
