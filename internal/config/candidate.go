package config

// Candidate represents a validated configuration with its canonical form and metadata.
// All fields are private; use accessors to retrieve values.
type Candidate struct {
	config         Config
	canonical      []byte
	revisionID     RevisionID
	sourcePath     string
	baseGeneration uint64
}

// NewCandidate creates a new Candidate from a Config.
// It validates the config, canonicalizes it, and computes the revision ID.
// The sourcePath is converted to an absolute, cleaned path if nonblank.
// Returns an error if the config is invalid or source path cannot be resolved.
func NewCandidate(cfg Config, sourcePath string, baseGeneration uint64) (Candidate, error) {
	// Call Canonical exactly once (which validates the config)
	canonical, err := Canonical(cfg)
	if err != nil {
		return Candidate{}, err
	}

	// Compute revision ID
	revisionID := RevisionFor(canonical)

	// Convert sourcePath: if nonblank, make it absolute and clean; if blank, keep blank
	cleanSourcePath, err := resolveAbsolutePath(sourcePath, "source_path")
	if err != nil {
		return Candidate{}, err
	}

	return Candidate{
		config:         cfg,
		canonical:      canonical,
		revisionID:     revisionID,
		sourcePath:     cleanSourcePath,
		baseGeneration: baseGeneration,
	}, nil
}

// LoadCandidate loads a Config from a file and creates a Candidate.
// Returns an error if the file cannot be read or the config is invalid.
// Preserves DecodeFile errors.
func LoadCandidate(path string, baseGeneration uint64) (Candidate, error) {
	// Load config from file
	cfg, err := DecodeFile(path)
	if err != nil {
		return Candidate{}, err
	}

	// Call NewCandidate to handle validation, canonicalization, and path resolution
	return NewCandidate(cfg, path, baseGeneration)
}

// Config returns the underlying Config.
func (c Candidate) Config() Config {
	return c.config
}

// Canonical returns a copy of the canonical TOML bytes.
func (c Candidate) Canonical() []byte {
	// Return a copy to prevent external mutation
	result := make([]byte, len(c.canonical))
	copy(result, c.canonical)
	return result
}

// RevisionID returns the SHA-256-based revision ID.
func (c Candidate) RevisionID() RevisionID {
	return c.revisionID
}

// SourcePath returns the source path.
func (c Candidate) SourcePath() string {
	return c.sourcePath
}

// BaseGeneration returns the base generation number.
func (c Candidate) BaseGeneration() uint64 {
	return c.baseGeneration
}
