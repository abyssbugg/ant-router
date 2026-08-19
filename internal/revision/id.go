package revision

import (
	"crypto/sha256"
	"encoding/hex"
)

// ID is a string type representing a SHA-256 hash of configuration.
// It must be exactly 64 lowercase hexadecimal characters.
type ID string

// String returns the string representation of the ID.
func (id ID) String() string {
	return string(id)
}

// Valid checks if the ID is valid (exactly 64 lowercase hex characters).
// Returns false if invalid.
func (id ID) Valid() bool {
	s := id.String()
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

// For computes the SHA-256 hash of the given bytes and returns it as an ID.
// The returned ID is guaranteed to be 64 lowercase hex characters.
func For(data []byte) ID {
	hash := sha256.Sum256(data)
	hexStr := hex.EncodeToString(hash[:])
	return ID(hexStr)
}
