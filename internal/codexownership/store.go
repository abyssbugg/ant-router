package codexownership

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/abyssbugg/ant-router/internal/atomicfile"
	"github.com/abyssbugg/ant-router/internal/canonicaljson"
)

type Store struct {
	root  string
	write func(context.Context, string, []byte, fs.FileMode) (atomicfile.Result, error)
}

func NewStore(root string) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("invalid Codex ownership root")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid Codex ownership root")
	}
	return &Store{root: filepath.Join(root, "codex", "ant-router"), write: atomicfile.Write}, nil
}

func validateInput(input Input) error {
	if !filepath.IsAbs(input.ProfilePath) || !filepath.IsAbs(input.CatalogPath) || !filepath.IsAbs(input.RouterctlPath) || len(input.Profile) == 0 || len(input.Profile) > MaxProfileBytes || len(input.Catalog) == 0 || len(input.Catalog) > MaxCatalogBytes || input.CodexVersion == "" || input.CreatedAt.IsZero() {
		return errors.New("invalid Codex ownership input")
	}
	return nil
}

func generationID(input Input) string {
	hash := sha256.New()
	for _, value := range [][]byte{[]byte(input.ProfilePath), []byte(input.CatalogPath), input.Profile, input.Catalog, []byte(input.CodexVersion), []byte(input.RouterctlPath)} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write(value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (store *Store) Put(ctx context.Context, input Input) (Generation, error) {
	generation, err := store.Stage(ctx, input)
	if err != nil {
		return Generation{}, err
	}
	return store.Commit(ctx, generation.ID)
}

// Stage durably writes an immutable generation without publishing it as current.
func (store *Store) Stage(ctx context.Context, input Input) (Generation, error) {
	if store == nil || ctx == nil || validateInput(input) != nil {
		return Generation{}, errors.New("invalid Codex ownership input")
	}
	if err := ctx.Err(); err != nil {
		return Generation{}, err
	}
	input.CreatedAt = input.CreatedAt.UTC()
	id := generationID(input)
	generationRoot := filepath.Join(store.root, "revisions", id)
	if info, err := os.Lstat(generationRoot); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Generation{}, errors.New("Codex ownership store unavailable")
		}
		if existing, readErr := store.Generation(ctx, id); readErr == nil {
			return existing, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Generation{}, errors.New("Codex ownership store unavailable")
	}
	revisions := filepath.Join(store.root, "revisions")
	for _, dir := range []string{store.root, revisions, generationRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Generation{}, errors.New("Codex ownership store unavailable")
		}
		if err := secureDirectory(dir); err != nil {
			return Generation{}, errors.New("Codex ownership store unavailable")
		}
	}
	generation := Generation{ID: id, ProfilePath: input.ProfilePath, CatalogPath: input.CatalogPath, CodexVersion: input.CodexVersion, RouterctlPath: input.RouterctlPath, CreatedAt: input.CreatedAt, Profile: append([]byte(nil), input.Profile...), Catalog: append([]byte(nil), input.Catalog...)}
	metadata, err := marshalJSON(generation)
	if err != nil {
		return Generation{}, err
	}
	writes := []struct {
		path string
		data []byte
	}{
		{filepath.Join(generationRoot, "profile.toml"), input.Profile},
		{filepath.Join(generationRoot, "models.json"), input.Catalog},
		{filepath.Join(generationRoot, "generation.json"), metadata},
	}
	for _, write := range writes {
		if _, err := store.write(ctx, write.path, write.data, 0o600); err != nil {
			return Generation{}, errors.New("Codex ownership store unavailable")
		}
		if err := secureFile(write.path); err != nil {
			return Generation{}, errors.New("Codex ownership store unavailable")
		}
	}
	return cloneGeneration(generation), nil
}

// Commit publishes a previously staged generation as current.
func (store *Store) Commit(ctx context.Context, id string) (Generation, error) {
	if store == nil || ctx == nil || !validID(id) {
		return Generation{}, errors.New("invalid Codex ownership generation")
	}
	if err := ctx.Err(); err != nil {
		return Generation{}, err
	}
	generation, err := store.Generation(ctx, id)
	if err != nil {
		return Generation{}, err
	}
	previous := ""
	if current, err := store.readManifest(ctx); err == nil {
		if current.Current == id {
			return generation, nil
		}
		if current.ProfilePath != generation.ProfilePath || current.CatalogPath != generation.CatalogPath {
			return Generation{}, errors.New("Codex ownership path conflict")
		}
		previous = current.Current
	} else if !errors.Is(err, os.ErrNotExist) {
		return Generation{}, err
	}
	manifest := Manifest{SchemaVersion: 1, Current: id, Previous: previous, ProfilePath: generation.ProfilePath, CatalogPath: generation.CatalogPath}
	encoded, err := marshalJSON(manifest)
	if err != nil {
		return Generation{}, err
	}
	manifestPath := filepath.Join(store.root, "ownership.json")
	result, err := store.write(ctx, manifestPath, encoded, 0o600)
	if err != nil {
		if result.Committed {
			return cloneGeneration(generation), committedError(err)
		}
		return Generation{}, errors.New("Codex ownership store unavailable")
	}
	if err := secureFile(manifestPath); err != nil {
		return cloneGeneration(generation), committedError(err)
	}
	if err := store.prune(ctx, manifest); err != nil {
		return cloneGeneration(generation), committedError(err)
	}
	return cloneGeneration(generation), nil
}

func marshalJSON(value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, errors.New("Codex ownership encoding failed")
	}
	return append(encoded, '\n'), nil
}

func (store *Store) Current(ctx context.Context) (Manifest, Generation, error) {
	manifest, err := store.readManifest(ctx)
	if err != nil {
		return Manifest{}, Generation{}, err
	}
	generation, err := store.Generation(ctx, manifest.Current)
	if err != nil {
		return Manifest{}, Generation{}, err
	}
	return manifest, generation, nil
}

func (store *Store) Generation(ctx context.Context, id string) (Generation, error) {
	if store == nil || ctx == nil || !validID(id) {
		return Generation{}, errors.New("invalid Codex ownership generation")
	}
	root := filepath.Join(store.root, "revisions", id)
	metadataBytes, err := readBounded(ctx, filepath.Join(root, "generation.json"), MaxManifestBytes)
	if err != nil {
		return Generation{}, err
	}
	var generation Generation
	if err := decodeStrict(metadataBytes, &generation); err != nil {
		return Generation{}, err
	}
	profile, err := readBounded(ctx, filepath.Join(root, "profile.toml"), MaxProfileBytes)
	if err != nil {
		return Generation{}, err
	}
	catalog, err := readBounded(ctx, filepath.Join(root, "models.json"), MaxCatalogBytes)
	if err != nil {
		return Generation{}, err
	}
	input := Input{ProfilePath: generation.ProfilePath, CatalogPath: generation.CatalogPath, Profile: profile, Catalog: catalog, CodexVersion: generation.CodexVersion, RouterctlPath: generation.RouterctlPath, CreatedAt: generation.CreatedAt}
	if generation.ID != id || generationID(input) != id {
		return Generation{}, errors.New("invalid Codex ownership generation")
	}
	generation.Profile, generation.Catalog = profile, catalog
	return cloneGeneration(generation), nil
}

func (store *Store) readManifest(ctx context.Context) (Manifest, error) {
	if store == nil || ctx == nil {
		return Manifest{}, errors.New("invalid Codex ownership store")
	}
	data, err := readBounded(ctx, filepath.Join(store.root, "ownership.json"), MaxManifestBytes)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := decodeStrict(data, &manifest); err != nil || manifest.SchemaVersion != 1 || !validID(manifest.Current) || (manifest.Previous != "" && !validID(manifest.Previous)) || !filepath.IsAbs(manifest.ProfilePath) || !filepath.IsAbs(manifest.CatalogPath) {
		return Manifest{}, errors.New("invalid Codex ownership manifest")
	}
	return manifest, nil
}

func (store *Store) prune(ctx context.Context, manifest Manifest) error {
	entries, err := os.ReadDir(filepath.Join(store.root, "revisions"))
	if err != nil {
		return errors.New("Codex ownership cleanup failed")
	}
	keep := map[string]bool{manifest.Current: true}
	if manifest.Previous != "" {
		keep[manifest.Previous] = true
	}
	var candidates []Generation
	revisionsRoot := filepath.Join(store.root, "revisions")
	removeStale := func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(revisionsRoot, name)); err != nil {
			return errors.New("Codex ownership cleanup failed")
		}
		return nil
	}
	for _, entry := range entries {
		name := entry.Name()
		if keep[name] {
			if !entry.IsDir() || !validID(name) {
				return errors.New("Codex ownership cleanup failed")
			}
			continue
		}
		if !entry.IsDir() || !validID(name) {
			if err := removeStale(name); err != nil {
				return err
			}
			continue
		}
		generation, err := store.Generation(ctx, name)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			if err := removeStale(name); err != nil {
				return err
			}
			continue
		}
		candidates = append(candidates, generation)
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].CreatedAt.Equal(candidates[right].CreatedAt) {
			return candidates[left].ID > candidates[right].ID
		}
		return candidates[left].CreatedAt.After(candidates[right].CreatedAt)
	})
	remaining := MaxGenerations - len(keep)
	for index, generation := range candidates {
		if index < remaining {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(store.root, "revisions", generation.ID)); err != nil {
			return errors.New("Codex ownership cleanup failed")
		}
	}
	return nil
}

func readBounded(ctx context.Context, path string, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > int64(limit) {
		return nil, errors.New("invalid Codex ownership file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("invalid Codex ownership file")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func decodeStrict(data []byte, target any) error {
	validated, err := canonicaljson.Parse(data, canonicaljson.Limits{MaximumBytes: MaxManifestBytes, MaximumDepth: 16})
	if err != nil {
		return errors.New("invalid Codex ownership data")
	}
	decoder := json.NewDecoder(bytes.NewReader(validated.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid Codex ownership data")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("invalid Codex ownership data")
	}
	return nil
}

func validID(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func cloneGeneration(value Generation) Generation {
	value.Profile = append([]byte(nil), value.Profile...)
	value.Catalog = append([]byte(nil), value.Catalog...)
	return value
}
