package credential

import (
	"context"
	"errors"
	"os"
	"strings"
)

// Source resolves credential references at request time.
type Source interface {
	Resolve(context.Context, Ref) (Secret, error)
}

// EnvSource resolves credentials from the process environment.
type EnvSource struct {
	lookup func(string) (string, bool)
}

// NewEnvSource constructs an environment source. A nil lookup uses os.LookupEnv.
func NewEnvSource(lookup func(string) (string, bool)) *EnvSource {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	return &EnvSource{lookup: lookup}
}

// Resolve looks up the referenced value on every call so rotation is observed.
func (s *EnvSource) Resolve(ctx context.Context, ref Ref) (Secret, error) {
	if s == nil || s.lookup == nil {
		return Secret{}, newError(CodeInvalidSource, "credential.source", nil)
	}
	if ctx == nil {
		return Secret{}, newError(CodeInvalidContext, "credential", nil)
	}
	if !ref.Valid() {
		return Secret{}, newError(CodeInvalidReference, "credential", nil)
	}
	if err := ctx.Err(); err != nil {
		return Secret{}, newError(CodeCancelled, "credential", err)
	}
	value, found := s.lookup(ref.Name())
	if err := ctx.Err(); err != nil {
		return Secret{}, newError(CodeCancelled, "credential", err)
	}
	if !found || strings.TrimSpace(value) == "" {
		return Secret{}, newError(CodeMissing, "credential", errors.New("credential is unavailable"))
	}
	return newSecret(value), nil
}
