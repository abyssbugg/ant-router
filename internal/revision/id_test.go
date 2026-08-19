package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestIDString(t *testing.T) {
	tests := []struct {
		name string
		id   ID
		want string
	}{
		{
			name: "lowercase hex",
			id:   ID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd"),
			want: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
		},
		{
			name: "empty",
			id:   ID(""),
			want: "",
		},
		{
			name: "all zeros",
			id:   ID("0000000000000000000000000000000000000000000000000000000000000000"),
			want: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.id.String()
			if got != tt.want {
				t.Errorf("ID.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIDValid(t *testing.T) {
	tests := []struct {
		name string
		id   ID
		want bool
	}{
		{
			name: "valid lowercase hex 64 chars",
			id:   ID("abc123def456abc123def456abc123def456abc123def456abc123def456abcd"),
			want: true,
		},
		{
			name: "uppercase rejects",
			id:   ID("ABC123DEF456ABC123DEF456ABC123DEF456ABC123DEF456ABC123DEF456ABCD"),
			want: false,
		},
		{
			name: "mixed case rejects",
			id:   ID("abc123DEF456abc123def456abc123def456abc123def456abc123def456abcd"),
			want: false,
		},
		{
			name: "too short",
			id:   ID("abc123def456abc123def456abc123def456abc123def456abc123def456abc"),
			want: false,
		},
		{
			name: "too long",
			id:   ID("abc123def456abc123def456abc123def456abc123def456abc123def456abcde"),
			want: false,
		},
		{
			name: "non-hex character",
			id:   ID("xyz123def456abc123def456abc123def456abc123def456abc123def456abcd"),
			want: false,
		},
		{
			name: "empty",
			id:   ID(""),
			want: false,
		},
		{
			name: "all zeros valid",
			id:   ID("0000000000000000000000000000000000000000000000000000000000000000"),
			want: true,
		},
		{
			name: "all f valid",
			id:   ID("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.id.Valid()
			if got != tt.want {
				t.Errorf("ID.Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFor(t *testing.T) {
	data := []byte("test data")
	id := For(data)

	// Verify format: 64 lowercase hex characters
	if len(id.String()) != 64 {
		t.Errorf("ID length = %d, want 64", len(id.String()))
	}

	// Verify it's valid
	if !id.Valid() {
		t.Errorf("ID.Valid() = false, want true")
	}

	// Verify it matches SHA-256
	hash := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(hash[:])
	if id.String() != expectedHex {
		t.Errorf("ID mismatch: got %s, want %s", id.String(), expectedHex)
	}
}

func TestForDeterministic(t *testing.T) {
	data := []byte("determinism test")
	id1 := For(data)
	id2 := For(data)

	if id1 != id2 {
		t.Errorf("For() not deterministic: got %s and %s", id1, id2)
	}
}

func TestForDifferentData(t *testing.T) {
	id1 := For([]byte("data1"))
	id2 := For([]byte("data2"))

	if id1 == id2 {
		t.Errorf("For() returned same ID for different data: %s == %s", id1, id2)
	}
}

func TestIDEmptyData(t *testing.T) {
	id := For([]byte(""))
	if !id.Valid() {
		t.Errorf("For(empty bytes).Valid() = false, want true")
	}
}
