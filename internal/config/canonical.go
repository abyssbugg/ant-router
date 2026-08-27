package config

import (
	"bytes"
	"fmt"

	"github.com/abyssbugg/ant-router/internal/revision"
	"github.com/pelletier/go-toml/v2"
)

// RevisionID is an alias for revision.ID to preserve the config API.
type RevisionID = revision.ID

// Canonical validates the config and produces canonical TOML bytes for hashing.
// The output is deterministic for the current implementation.
//
// Determinism guarantee: The current schema is map-free (all config values are
// scalar or fixed-structure types), encoding is pinned to go-toml/v2 v2.4.3,
// and byte stability is guarded by TestCanonicalDeterministic and related tests.
// This does NOT claim determinism for arbitrary maps or future versions.
// Callers MUST NOT rely on byte stability across schema changes or encoder upgrades.
// For stable hashing across versions, use a higher-level protocol with explicit
// version markers and semantic hashing.
//
// Returns an error if the config is invalid.
func Canonical(cfg Config) ([]byte, error) {
	cfg = cfg.normalized()
	// Validate the config first
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Encode to TOML using go-toml encoder
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)

	// The encoder produces deterministic output for the pinned version and schema.
	if err := encoder.Encode(cfg); err != nil {
		return nil, &Error{
			Code: CodeDecodeFailed,
			Path: "",
			Err:  fmt.Errorf("failed to encode canonical TOML: %v", err),
		}
	}

	return buf.Bytes(), nil
}

// RevisionFor computes the SHA-256 hash of the given bytes and returns it as a RevisionID.
// The returned RevisionID is guaranteed to be 64 lowercase hex characters.
func RevisionFor(data []byte) RevisionID {
	return RevisionID(revision.For(data))
}
