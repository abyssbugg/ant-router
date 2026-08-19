package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// RevisionID is a string type representing a SHA-256 hash of canonical configuration.
// It must be exactly 64 lowercase hexadecimal characters.
type RevisionID string

// String returns the string representation of the RevisionID.
func (rid RevisionID) String() string {
	return string(rid)
}

// Valid checks if the RevisionID is valid (exactly 64 lowercase hex characters).
// Returns false if invalid.
func (rid RevisionID) Valid() bool {
	s := rid.String()
	if len(s) != 64 {
		return false
	}

	// Check if all characters are valid lowercase hex
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}

	return true
}

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
	hash := sha256.Sum256(data)
	hexStr := hex.EncodeToString(hash[:])
	return RevisionID(hexStr)
}
