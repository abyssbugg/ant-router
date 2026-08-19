package configstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/abyssbugg/ant-router/internal/revision"
)

const ManifestFormatVersion = 1

// validateNoDuplicateKeys checks that a JSON object has no duplicate keys.
// Returns an error if duplicate keys are found or if the JSON is not a single object.
func validateNoDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))

	// Expect an object token
	t, err := decoder.Token()
	if err != nil {
		return errors.New("invalid manifest JSON")
	}
	if t != json.Delim('{') {
		return errors.New("invalid manifest JSON")
	}

	seen := make(map[string]bool)
	for decoder.More() {
		t, err := decoder.Token()
		if err != nil {
			return errors.New("invalid manifest JSON")
		}

		// Token should be a key (string)
		key, ok := t.(string)
		if !ok {
			return errors.New("invalid manifest JSON")
		}

		// Check for duplicate
		if seen[key] {
			return errors.New("invalid manifest JSON")
		}
		seen[key] = true

		// Skip the value
		var v interface{}
		if err := decoder.Decode(&v); err != nil {
			return errors.New("invalid manifest JSON")
		}
	}

	// Expect closing object
	t, err = decoder.Token()
	if err != nil {
		return errors.New("invalid manifest JSON")
	}
	if t != json.Delim('}') {
		return errors.New("invalid manifest JSON")
	}

	// Ensure no trailing content
	if decoder.More() {
		return errors.New("invalid manifest JSON")
	}

	return nil
}

// Manifest represents the stored configuration manifest with revision tracking.
type Manifest struct {
	FormatVersion uint64      `json:"format_version"`
	Generation    uint64      `json:"generation"`
	Current       revision.ID `json:"current"`
	Previous      revision.ID `json:"previous,omitempty"`
	ActivatedAt   time.Time   `json:"activated_at"`
}

// Validate checks if the manifest is valid.
// Returns a typed error with CodeManifestCorrupt for structural issues
// and CodeInvalidRevision for invalid revision IDs.
func (m *Manifest) Validate() error {
	// Require format version 1
	if m.FormatVersion != ManifestFormatVersion {
		return &Error{
			Code: CodeManifestCorrupt,
			Path: "format_version",
			Err:  fmt.Errorf("expected format version %d, got %d", ManifestFormatVersion, m.FormatVersion),
		}
	}

	// Generation must be > 0
	if m.Generation == 0 {
		return &Error{
			Code: CodeManifestCorrupt,
			Path: "generation",
			Err:  fmt.Errorf("generation must be > 0"),
		}
	}

	// Current revision must be valid
	if !m.Current.Valid() {
		return &Error{
			Code: CodeInvalidRevision,
			Path: "current",
			Err:  fmt.Errorf("invalid revision ID"),
		}
	}

	// Previous revision must be blank or valid
	if m.Previous != "" && !m.Previous.Valid() {
		return &Error{
			Code: CodeInvalidRevision,
			Path: "previous",
			Err:  fmt.Errorf("invalid revision ID"),
		}
	}

	// ActivatedAt must be nonzero
	if m.ActivatedAt.IsZero() {
		return &Error{
			Code: CodeManifestCorrupt,
			Path: "activated_at",
			Err:  fmt.Errorf("activated_at must be set"),
		}
	}

	return nil
}

// EncodeManifest validates the manifest and encodes it as deterministic indented JSON with trailing newline.
func EncodeManifest(m Manifest) ([]byte, error) {
	// Normalize ActivatedAt to UTC before validation and encoding
	m.ActivatedAt = m.ActivatedAt.UTC()

	if err := m.Validate(); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(m); err != nil {
		return nil, &Error{
			Code: CodeManifestCorrupt,
			Path: "",
			Err:  errors.New("failed to encode manifest"),
		}
	}

	return buf.Bytes(), nil
}

// DecodeManifest decodes JSON bytes into a Manifest.
// Rejects empty input, malformed JSON, unknown fields, trailing JSON values,
// and validates the resulting manifest semantically.
// Does not echo input in error messages.
func DecodeManifest(data []byte) (Manifest, error) {
	// Reject empty input
	if len(data) == 0 {
		return Manifest{}, &Error{
			Code: CodeManifestCorrupt,
			Path: "",
			Err:  fmt.Errorf("empty input"),
		}
	}

	// Check for duplicate keys
	if err := validateNoDuplicateKeys(data); err != nil {
		return Manifest{}, &Error{
			Code: CodeManifestCorrupt,
			Path: "",
			Err:  err,
		}
	}

	// Use a decoder with strict settings
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var m Manifest
	if err := decoder.Decode(&m); err != nil {
		return Manifest{}, &Error{
			Code: CodeManifestCorrupt,
			Path: "",
			Err:  errors.New("invalid manifest JSON"),
		}
	}

	var trailing any
	secondDecodeErr := decoder.Decode(&trailing)
	if !errors.Is(secondDecodeErr, io.EOF) {
		return Manifest{}, &Error{
			Code: CodeManifestCorrupt,
			Path: "",
			Err:  errors.New("unexpected trailing content in JSON"),
		}
	}

	// Normalize ActivatedAt to UTC before returning
	m.ActivatedAt = m.ActivatedAt.UTC()

	// Validate the manifest semantically
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}

	return m, nil
}
