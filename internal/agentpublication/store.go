package agentpublication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
	"github.com/abyssbugg/ant-router/internal/filelock"
)

const (
	// MaxSettingsBytes bounds a settings file the publication will touch.
	MaxSettingsBytes = 512 << 10
	maxGenerations   = 10
	generationIDTag  = "ant-router-agent-generation-v1"
	journalSchema    = 1
)

var (
	errStoreUnavailable  = errors.New("agent publication store is unavailable")
	errStoreCorrupt      = errors.New("agent publication store is corrupt")
	errDurabilityUnknown = errors.New("agent publication durability is uncertain")
)

// Store keeps content-addressed generations of (pre-image, post-image)
// settings pairs for one agent target, plus the current/previous manifest.
// Layout under root:
//
//	<root>/ownership.json            current/previous manifest
//	<root>/revisions/<64-hex>/       original.json, merged.json, generation.json
//	<root>/pending.json              in-flight journal (only while mutating)
type Store struct {
	root string
	now  func() time.Time
}

// NewStore resolves the store directory for one agent target. subdir is the
// agent-scoped publication name (e.g. "gemini/ant-router").
func NewStore(root, subdir string) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, errStoreUnavailable
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errStoreUnavailable
	}
	return &Store{root: filepath.Join(root, filepath.FromSlash(subdir)), now: time.Now}, nil
}

// Generation is one stored publication: the exact pre-image bytes, the exact
// merged bytes, and the target metadata.
type Generation struct {
	ID           string    `json:"id"`
	SettingsPath string    `json:"settings_path"`
	Agent        string    `json:"agent"`
	CreatedAt    time.Time `json:"created_at"`

	original []byte
	merged   []byte
}

// Original returns the pre-image bytes captured before the publication.
func (g *Generation) Original() []byte { return append([]byte(nil), g.original...) }

// Merged returns the published bytes.
func (g *Generation) Merged() []byte { return append([]byte(nil), g.merged...) }

type ownershipManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Current       string `json:"current"`
	Previous      string `json:"previous,omitempty"`
}

type storedGeneration struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	SettingsPath  string    `json:"settings_path"`
	Agent         string    `json:"agent"`
	CreatedAt     time.Time `json:"created_at"`
}

func generationID(settingsPath, agent string, original, merged []byte) string {
	hash := sha256.New()
	writeField := func(b []byte) {
		var length [8]byte
		for i := range length {
			length[i] = byte(len(b) >> (8 * i))
		}
		hash.Write(length[:])
		hash.Write(b)
	}
	writeField([]byte(generationIDTag))
	writeField([]byte(settingsPath))
	writeField([]byte(agent))
	writeField(original)
	writeField(merged)
	return hex.EncodeToString(hash.Sum(nil))
}

func (s *Store) manifestPath() string { return filepath.Join(s.root, "ownership.json") }
func (s *Store) revisionsDir() string { return filepath.Join(s.root, "revisions") }
func (s *Store) pendingPath() string  { return filepath.Join(s.root, "pending.json") }
func (s *Store) lockPath() string     { return filepath.Join(s.root, "mutation.lock") }
func (s *Store) originalPath(id string) string {
	return filepath.Join(s.revisionsDir(), id, "original-settings.json")
}
func (s *Store) mergedPath(id string) string {
	return filepath.Join(s.revisionsDir(), id, "merged-settings.json")
}
func (s *Store) metaPath(id string) string {
	return filepath.Join(s.revisionsDir(), id, "generation.json")
}

// Stage stores a generation for the given pre-image and merged bytes without
// publishing it. Idempotent: identical content returns the existing generation.
func (s *Store) Stage(settingsPath, agent string, original, merged []byte) (*Generation, error) {
	if len(original) > MaxSettingsBytes || len(merged) > MaxSettingsBytes {
		return nil, fmt.Errorf("%w: settings exceed %d bytes", errStoreCorrupt, MaxSettingsBytes)
	}
	id := generationID(settingsPath, agent, original, merged)
	dir := filepath.Join(s.revisionsDir(), id)
	if _, err := os.Lstat(dir); err == nil {
		if err := s.verifyGeneration(id, settingsPath, agent, original, merged); err != nil {
			return nil, err
		}
		return s.Load(id)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errStoreUnavailable
	}
	generation := storedGeneration{SchemaVersion: 1, ID: id, SettingsPath: settingsPath, Agent: agent, CreatedAt: s.now().UTC()}
	meta, err := json.MarshalIndent(generation, "", "  ")
	if err != nil {
		return nil, errStoreUnavailable
	}
	meta = append(meta, '\n')
	if _, err := atomicfile.Write(context.Background(), filepath.Join(dir, "generation.json"), meta, 0o600); err != nil {
		return nil, errStoreUnavailable
	}
	if _, err := atomicfile.Write(context.Background(), s.originalPath(id), original, 0o600); err != nil {
		return nil, errStoreUnavailable
	}
	if _, err := atomicfile.Write(context.Background(), s.mergedPath(id), merged, 0o600); err != nil {
		return nil, errStoreUnavailable
	}
	if err := s.verifyGeneration(id, settingsPath, agent, original, merged); err != nil {
		return nil, err
	}
	return s.Load(id)
}

func (s *Store) verifyGeneration(id, settingsPath, agent string, original, merged []byte) error {
	raw, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return errStoreCorrupt
	}
	var generation storedGeneration
	if err := json.Unmarshal(raw, &generation); err != nil || generation.ID != id || generation.SettingsPath != settingsPath || generation.Agent != agent {
		return errStoreCorrupt
	}
	storedOriginal, err := os.ReadFile(s.originalPath(id))
	if err != nil || string(storedOriginal) != string(original) {
		return errStoreCorrupt
	}
	storedMerged, err := os.ReadFile(s.mergedPath(id))
	if err != nil || string(storedMerged) != string(merged) {
		return errStoreCorrupt
	}
	return nil
}

// Load reads one generation by id, re-verifying its content hash.
func (s *Store) Load(id string) (*Generation, error) {
	if !validID(id) {
		return nil, errStoreCorrupt
	}
	raw, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return nil, errStoreCorrupt
	}
	var generation storedGeneration
	if err := json.Unmarshal(raw, &generation); err != nil || generation.ID != id || generation.SchemaVersion != 1 {
		return nil, errStoreCorrupt
	}
	original, err := os.ReadFile(s.originalPath(id))
	if err != nil {
		return nil, errStoreCorrupt
	}
	merged, err := os.ReadFile(s.mergedPath(id))
	if err != nil {
		return nil, errStoreCorrupt
	}
	expected := generationID(generation.SettingsPath, generation.Agent, original, merged)
	if expected != id {
		return nil, errStoreCorrupt
	}
	return &Generation{ID: id, SettingsPath: generation.SettingsPath, Agent: generation.Agent, CreatedAt: generation.CreatedAt, original: original, merged: merged}, nil
}

// Commit publishes a staged generation as current, keeping the previous one.
func (s *Store) Commit(id string) error {
	if _, err := s.Load(id); err != nil {
		return err
	}
	manifest, err := s.readManifest()
	if err != nil {
		return err
	}
	previous := manifest.Previous
	if manifest.Current != "" && manifest.Current != id {
		previous = manifest.Current
	}
	next := ownershipManifest{SchemaVersion: 1, Current: id, Previous: previous}
	encoded, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return errStoreUnavailable
	}
	encoded = append(encoded, '\n')
	if _, err := atomicfile.Write(context.Background(), s.manifestPath(), encoded, 0o600); err != nil {
		if _, statErr := os.Lstat(s.manifestPath()); statErr == nil {
			return fmt.Errorf("%w: manifest may or may not have been written", errDurabilityUnknown)
		}
		return errStoreUnavailable
	}
	s.prune()
	return nil
}

func (s *Store) readManifest() (ownershipManifest, error) {
	raw, err := os.ReadFile(s.manifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return ownershipManifest{SchemaVersion: 1}, nil
	}
	if err != nil {
		return ownershipManifest{}, errStoreUnavailable
	}
	var manifest ownershipManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.SchemaVersion != 1 {
		return ownershipManifest{}, errStoreCorrupt
	}
	return manifest, nil
}

// Current returns the committed generation, if any.
func (s *Store) Current() (*Generation, bool, error) {
	manifest, err := s.readManifest()
	if err != nil {
		return nil, false, err
	}
	if manifest.Current == "" {
		return nil, false, nil
	}
	generation, loadErr := s.Load(manifest.Current)
	if loadErr != nil {
		return nil, true, loadErr
	}
	return generation, true, nil
}

// Previous returns the committed previous generation, if any.
func (s *Store) Previous() (*Generation, bool, error) {
	manifest, err := s.readManifest()
	if err != nil {
		return nil, false, err
	}
	if manifest.Previous == "" {
		return nil, false, nil
	}
	generation, loadErr := s.Load(manifest.Previous)
	if loadErr != nil {
		return nil, true, loadErr
	}
	return generation, true, nil
}

func (s *Store) prune() {
	manifest, manifestErr := s.readManifest()
	if manifestErr != nil {
		return
	}
	entries, err := os.ReadDir(s.revisionsDir())
	if err != nil {
		return
	}
	type dated struct {
		id string
		at time.Time
	}
	var kept []dated
	for _, entry := range entries {
		id := entry.Name()
		if !validID(id) || id == manifest.Current || id == manifest.Previous {
			continue
		}
		generation, err := s.Load(id)
		if err != nil {
			_ = os.RemoveAll(filepath.Join(s.revisionsDir(), id))
			continue
		}
		kept = append(kept, dated{id: id, at: generation.CreatedAt})
	}
	if len(kept) <= maxGenerations {
		return
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].at.Before(kept[j].at) })
	for _, entry := range kept[:len(kept)-maxGenerations] {
		_ = os.RemoveAll(filepath.Join(s.revisionsDir(), entry.id))
	}
}

func validID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// AcquireLock takes the cross-process mutation lock for this target.
func (s *Store) AcquireLock(ctx context.Context) (*filelock.Lock, error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, errStoreUnavailable
	}
	return filelock.Acquire(ctx, s.lockPath())
}

// WritePending records the in-flight journal before any settings mutation.
type pendingJournal struct {
	SchemaVersion int    `json:"schema_version"`
	Action        string `json:"action"`
	GenerationID  string `json:"generation_id"`
	SettingsPath  string `json:"settings_path"`
	Agent         string `json:"agent"`
}

func (s *Store) WritePending(action, generationID, settingsPath, agent string) error {
	journal := pendingJournal{SchemaVersion: journalSchema, Action: action, GenerationID: generationID, SettingsPath: settingsPath, Agent: agent}
	encoded, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return errStoreUnavailable
	}
	encoded = append(encoded, '\n')
	if _, err := atomicfile.Write(context.Background(), s.pendingPath(), encoded, 0o600); err != nil {
		return errStoreUnavailable
	}
	return nil
}

// ReadPending returns the in-flight journal, if any.
func (s *Store) ReadPending() (pendingJournal, bool, error) {
	raw, err := os.ReadFile(s.pendingPath())
	if errors.Is(err, os.ErrNotExist) {
		return pendingJournal{}, false, nil
	}
	if err != nil {
		return pendingJournal{}, false, errStoreUnavailable
	}
	var journal pendingJournal
	if err := json.Unmarshal(raw, &journal); err != nil || journal.SchemaVersion != journalSchema {
		return pendingJournal{}, false, errStoreCorrupt
	}
	return journal, true, nil
}

// ClearPending removes the in-flight journal.
func (s *Store) ClearPending() error {
	if err := os.Remove(s.pendingPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errStoreUnavailable
	}
	return nil
}

// Retract clears the current generation, moving it to previous so Restore
// can republish it. The settings file itself is not touched.
func (s *Store) Retract() error {
	manifest, err := s.readManifest()
	if err != nil {
		return err
	}
	if manifest.Current == "" {
		return nil
	}
	next := ownershipManifest{SchemaVersion: 1, Current: "", Previous: manifest.Current}
	encoded, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return errStoreUnavailable
	}
	encoded = append(encoded, '\n')
	if _, err := atomicfile.Write(context.Background(), s.manifestPath(), encoded, 0o600); err != nil {
		if _, statErr := os.Lstat(s.manifestPath()); statErr == nil {
			return fmt.Errorf("%w: manifest may or may not have been written", errDurabilityUnknown)
		}
		return errStoreUnavailable
	}
	s.prune()
	return nil
}
