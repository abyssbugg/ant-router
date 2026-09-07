package codexintegration

import "errors"

var (
	ErrInvalid     = errors.New("invalid Codex integration request")
	ErrConflict    = errors.New("Codex integration conflict")
	ErrUnavailable = errors.New("Codex integration unavailable")
)
