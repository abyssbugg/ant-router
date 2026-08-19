package configstore

import "errors"

// Code represents an error code for configstore errors
type Code string

const (
	CodeInvalidRevision     Code = "invalid_revision"
	CodeRevisionCorrupt     Code = "revision_corrupt"
	CodeManifestCorrupt     Code = "manifest_corrupt"
	CodeManifestNotFound    Code = "manifest_not_found"
	CodeStoreUnavailable    Code = "store_unavailable"
	CodeCancelled           Code = "cancelled"
	CodeDurabilityUncertain Code = "durability_uncertain"
)

// Error represents a configstore error with context
type Error struct {
	Code Code
	Path string
	Err  error
}

// Error implements the error interface and returns stable string even when Err is nil
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	errText := ""
	if e.Err != nil {
		errText = e.Err.Error()
	}

	if e.Path != "" {
		if errText != "" {
			return e.Code.String() + " at " + e.Path + ": " + errText
		}
		return e.Code.String() + " at " + e.Path
	}
	if errText != "" {
		return e.Code.String() + ": " + errText
	}
	return e.Code.String()
}

// Unwrap implements the errors.Unwrap interface for error chain traversal
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// String returns the string representation of a Code
func (c Code) String() string {
	return string(c)
}

// CodeOf extracts the Code from an error if it's a *Error, including through error chains with fmt.Errorf %w
func CodeOf(err error) (Code, bool) {
	var e *Error
	if errors.As(err, &e) && e != nil {
		return e.Code, true
	}
	return "", false
}
