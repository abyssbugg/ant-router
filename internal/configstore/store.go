package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/abyssbugg/ant-router/internal/revision"
)

var (
	errRevisionNotFound = errors.New("revision not found")
	errFileTooLarge     = errors.New("file exceeds maximum size")
)

const (
	// MaxRevisionBytes is the maximum size of a revision in bytes (1 MiB).
	MaxRevisionBytes = 1 << 20
	// MaxManifestBytes is the maximum size of a manifest in bytes (64 KiB).
	MaxManifestBytes = 64 << 10
)

// Store defines the interface for persisting revisions and manifest.
type Store interface {
	// PutRevision stores a revision by ID. Returns error if ID is invalid,
	// data size exceeds MaxRevisionBytes, or hash doesn't match.
	PutRevision(context.Context, revision.ID, []byte) error

	// ReadRevision reads a revision by ID. Returns error if ID is invalid,
	// file is missing/corrupt/oversized/symlink/directory, or read fails.
	ReadRevision(context.Context, revision.ID) ([]byte, error)

	// ReadManifest reads the current manifest. Returns CodeManifestNotFound
	// if manifest is missing, or CodeManifestCorrupt for other issues.
	ReadManifest(context.Context) (Manifest, error)

	// WriteManifest writes the manifest atomically. Verifies that Current and
	// nonblank Previous revisions exist before writing.
	WriteManifest(context.Context, Manifest) error

	// CleanupTemps removes temporary files matching the atomic write pattern
	// (.<base>.tmp-*) from root and revisions directories.
	CleanupTemps(context.Context) error
	RepairRevision(context.Context, revision.ID, []byte) error
}

// FileStore is an implementation of Store that persists data to the filesystem.
type FileStore struct {
	mutationMu   sync.Mutex
	root         string                                                   // absolute path to root directory
	revisionsDir string                                                   // absolute path to revisions directory
	manifestPath string                                                   // absolute path to manifest file
	writeFile    func(context.Context, string, []byte, fs.FileMode) error // injectable write function
	readFile     func(string, int) ([]byte, error)                        // injectable bounded read
}

// NewFileStore creates a new FileStore at the given root directory.
// Creates root and revisions directories with owner-only permissions (0700).
// Rejects blank paths, non-directory paths, and symlinks.
func NewFileStore(root string) (*FileStore, error) {
	// Reject blank path
	if root == "" {
		return nil, &Error{Code: CodeStoreUnavailable, Err: os.ErrInvalid}
	}

	// Convert to absolute path
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, &Error{Code: CodeStoreUnavailable, Err: err}
	}

	// Clean the path
	absRoot = filepath.Clean(absRoot)

	// Check if root already exists
	rootStat, rootErr := os.Lstat(absRoot)
	if rootErr == nil {
		// Root exists, verify it's a directory and not a symlink
		if !rootStat.IsDir() {
			return nil, &Error{Code: CodeStoreUnavailable, Err: os.ErrInvalid}
		}
		if rootStat.Mode()&os.ModeSymlink != 0 {
			return nil, &Error{Code: CodeStoreUnavailable, Err: os.ErrInvalid}
		}
	} else if !os.IsNotExist(rootErr) {
		return nil, &Error{Code: CodeStoreUnavailable, Err: rootErr}
	} else {
		// Root doesn't exist, create it with 0700 permissions
		if err := os.MkdirAll(absRoot, 0700); err != nil {
			return nil, &Error{Code: CodeStoreUnavailable, Err: err}
		}
	}
	if err := secureDirectory(absRoot); err != nil {
		return nil, &Error{Code: CodeStoreUnavailable, Path: absRoot, Err: err}
	}

	// Revisions directory path
	revisionsDir := filepath.Join(absRoot, "revisions")

	// Check if revisions directory already exists
	revisionsStat, revisionsErr := os.Lstat(revisionsDir)
	if revisionsErr == nil {
		// Revisions dir exists, verify it's a directory and not a symlink
		if !revisionsStat.IsDir() {
			return nil, &Error{Code: CodeStoreUnavailable, Err: os.ErrInvalid}
		}
		if revisionsStat.Mode()&os.ModeSymlink != 0 {
			return nil, &Error{Code: CodeStoreUnavailable, Err: os.ErrInvalid}
		}
	} else if !os.IsNotExist(revisionsErr) {
		return nil, &Error{Code: CodeStoreUnavailable, Err: revisionsErr}
	} else {
		// Revisions dir doesn't exist, create it with 0700 permissions
		if err := os.Mkdir(revisionsDir, 0700); err != nil {
			return nil, &Error{Code: CodeStoreUnavailable, Err: err}
		}
	}
	if err := secureDirectory(revisionsDir); err != nil {
		return nil, &Error{Code: CodeStoreUnavailable, Path: revisionsDir, Err: err}
	}

	manifestPath := filepath.Join(absRoot, "manifest.json")

	fs := &FileStore{
		root:         absRoot,
		revisionsDir: revisionsDir,
		manifestPath: manifestPath,
	}

	// Set injectable filesystem operations.
	fs.writeFile = writeAtomic
	fs.readFile = readBoundedFile

	return fs, nil
}

// PutRevision stores a revision by ID with the given data.
// Validates context, ID, and data size. Verifies that revision.For(data) == id.
// Creates file at <root>/revisions/<id>.toml with 0600 permissions.
// If file exists and is valid, operation is idempotent.
func (fs *FileStore) PutRevision(ctx context.Context, id revision.ID, data []byte) error {
	fs.mutationMu.Lock()
	defer fs.mutationMu.Unlock()

	// Check context
	select {
	case <-ctx.Done():
		return &Error{Code: CodeCancelled, Err: ctx.Err()}
	default:
	}

	// Validate ID
	if !id.Valid() {
		return &Error{Code: CodeInvalidRevision, Err: fmt.Errorf("invalid revision ID")}
	}

	// Validate data size
	if len(data) > MaxRevisionBytes {
		return &Error{Code: CodeStoreUnavailable, Err: fmt.Errorf("revision data exceeds maximum size")}
	}

	// Verify hash matches
	if revision.For(data) != id {
		return &Error{Code: CodeInvalidRevision, Err: fmt.Errorf("data hash does not match revision ID")}
	}

	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")

	// Check if file already exists - if so, verify and skip write
	existingStat, existingErr := os.Lstat(revPath)
	if existingErr == nil {
		// File exists, verify it's a regular file (not symlink or dir)
		if existingStat.IsDir() {
			return &Error{Code: CodeRevisionCorrupt, Path: revPath, Err: fmt.Errorf("revision is a directory")}
		}
		if existingStat.Mode()&os.ModeSymlink != 0 {
			return &Error{Code: CodeRevisionCorrupt, Path: revPath, Err: fmt.Errorf("revision is a symlink")}
		}

		// Verify existing file matches the data
		existingData, err := fs.ReadRevision(ctx, id)
		if err != nil {
			return err
		}
		if bytes.Equal(existingData, data) {
			return nil
		}
		return &Error{Code: CodeRevisionCorrupt, Path: revPath, Err: fmt.Errorf("stored revision content differs")}
	} else if !os.IsNotExist(existingErr) {
		return &Error{Code: CodeStoreUnavailable, Path: revPath, Err: existingErr}
	}

	// Write file atomically
	return fs.writeFile(ctx, revPath, data, 0600)
}

// ReadRevision reads a revision by ID.
// Validates context and ID. Returns error for missing/oversize/corrupt/symlink/directory files.
// Verifies file hash matches ID. Returns defensive copy of data.
func (fs *FileStore) ReadRevision(ctx context.Context, id revision.ID) ([]byte, error) {
	// Check context
	select {
	case <-ctx.Done():
		return nil, &Error{Code: CodeCancelled, Err: ctx.Err()}
	default:
	}

	// Validate ID
	if !id.Valid() {
		return nil, &Error{Code: CodeInvalidRevision, Err: fmt.Errorf("invalid revision ID")}
	}

	revPath := filepath.Join(fs.revisionsDir, id.String()+".toml")

	// Use Lstat to reject symlinks
	stat, err := os.Lstat(revPath)
	if err != nil {
		if os.IsNotExist(err) {
			dirInfo, dirErr := os.Lstat(fs.revisionsDir)
			if dirErr != nil {
				return nil, &Error{Code: CodeStoreUnavailable, Path: fs.revisionsDir, Err: dirErr}
			}
			if !dirInfo.IsDir() {
				return nil, &Error{Code: CodeStoreUnavailable, Path: fs.revisionsDir, Err: os.ErrInvalid}
			}
			return nil, &Error{Code: CodeStoreUnavailable, Path: revPath, Err: errRevisionNotFound}
		}
		return nil, &Error{Code: CodeStoreUnavailable, Path: revPath, Err: err}
	}

	if !stat.Mode().IsRegular() {
		return nil, &Error{Code: CodeRevisionCorrupt, Path: revPath, Err: fmt.Errorf("revision is not a regular file")}
	}
	if !privateFilePermissions(revPath, stat.Mode()) {
		return nil, &Error{Code: CodeRevisionCorrupt, Path: revPath, Err: fmt.Errorf("revision permissions are not private")}
	}

	data, err := fs.readFile(revPath, MaxRevisionBytes)
	if err != nil {
		return nil, &Error{Code: CodeStoreUnavailable, Path: revPath, Err: err}
	}

	// Verify hash matches ID
	if revision.For(data) != id {
		return nil, &Error{Code: CodeRevisionCorrupt, Path: revPath, Err: fmt.Errorf("file hash does not match revision ID")}
	}

	// Return defensive copy
	defensiveCopy := make([]byte, len(data))
	copy(defensiveCopy, data)
	return defensiveCopy, nil
}

// ReadManifest reads the manifest file.
// Returns CodeManifestNotFound if missing.
// Returns CodeManifestCorrupt for symlinks, directories, or invalid JSON.
// Returns CodeStoreUnavailable for oversized files or read errors.
func (fs *FileStore) ReadManifest(ctx context.Context) (Manifest, error) {
	// Check context
	select {
	case <-ctx.Done():
		return Manifest{}, &Error{Code: CodeCancelled, Err: ctx.Err()}
	default:
	}

	// Use Lstat to reject symlinks
	stat, err := os.Lstat(fs.manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, &Error{Code: CodeManifestNotFound, Path: fs.manifestPath, Err: err}
		}
		return Manifest{}, &Error{Code: CodeStoreUnavailable, Path: fs.manifestPath, Err: err}
	}

	if !stat.Mode().IsRegular() {
		return Manifest{}, &Error{Code: CodeManifestCorrupt, Path: fs.manifestPath, Err: fmt.Errorf("manifest is not a regular file")}
	}
	if !privateFilePermissions(fs.manifestPath, stat.Mode()) {
		return Manifest{}, &Error{Code: CodeManifestCorrupt, Path: fs.manifestPath, Err: fmt.Errorf("manifest permissions are not private")}
	}

	data, err := fs.readFile(fs.manifestPath, MaxManifestBytes)
	if err != nil {
		return Manifest{}, &Error{Code: CodeStoreUnavailable, Path: fs.manifestPath, Err: err}
	}

	// Decode manifest
	manifest, err := DecodeManifest(data)
	if err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}

// WriteManifest writes the manifest atomically.
// Validates the manifest. Verifies Current and nonblank Previous revisions exist via ReadRevision.
// Encodes manifest and writes with fs.writeFile at 0600 permissions.
// Propagates CodeDurabilityUncertain unchanged.
func (fs *FileStore) WriteManifest(ctx context.Context, manifest Manifest) error {
	fs.mutationMu.Lock()
	defer fs.mutationMu.Unlock()

	// Check context
	select {
	case <-ctx.Done():
		return &Error{Code: CodeCancelled, Err: ctx.Err()}
	default:
	}

	// Validate manifest
	if err := manifest.Validate(); err != nil {
		return err
	}

	if err := fs.verifyManifestRevision(ctx, manifest.Current, "current"); err != nil {
		return err
	}
	if manifest.Previous != "" {
		if err := fs.verifyManifestRevision(ctx, manifest.Previous, "previous"); err != nil {
			return err
		}
	}

	// Encode manifest
	data, err := EncodeManifest(manifest)
	if err != nil {
		return err
	}

	return fs.writeFile(ctx, fs.manifestPath, data, 0o600)
}

func (fs *FileStore) verifyManifestRevision(ctx context.Context, id revision.ID, path string) error {
	_, err := fs.ReadRevision(ctx, id)
	if err == nil {
		return nil
	}
	code, _ := CodeOf(err)
	if errors.Is(err, errRevisionNotFound) || code == CodeRevisionCorrupt || code == CodeInvalidRevision {
		return &Error{Code: CodeManifestCorrupt, Path: path, Err: err}
	}
	return err
}

// CleanupTemps removes temporary files matching the atomic pattern (.<base>.tmp-*).
// Scans root and revisions directories. Removes regular files and symlinks.
// Ignores directories and unrelated hidden files.
// Honors context cancellation between entries.
func (fs *FileStore) CleanupTemps(ctx context.Context) error {
	fs.mutationMu.Lock()
	defer fs.mutationMu.Unlock()

	if err := contextError(ctx); err != nil {
		return err
	}
	if err := cleanupTempDirectory(ctx, fs.root, isManifestTempName); err != nil {
		return err
	}
	return cleanupTempDirectory(ctx, fs.revisionsDir, isRevisionTempName)
}

func cleanupTempDirectory(ctx context.Context, dir string, owned func(string) bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: dir, Err: err}
	}
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return err
		}
		if entry.IsDir() || !owned(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			return &Error{Code: CodeStoreUnavailable, Path: path, Err: err}
		}
	}
	return nil
}

func isManifestTempName(name string) bool {
	const prefix = ".manifest.json.tmp-"
	return strings.HasPrefix(name, prefix) && len(name) > len(prefix)
}

func isRevisionTempName(name string) bool {
	const marker = ".toml.tmp-"
	if !strings.HasPrefix(name, ".") {
		return false
	}
	markerIndex := strings.Index(name, marker)
	if markerIndex <= 1 || markerIndex+len(marker) == len(name) {
		return false
	}
	return revision.ID(name[1:markerIndex]).Valid()
}

func readBoundedFile(path string, maximum int) ([]byte, error) {
	file, err := openManagedFile(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > maximum {
		return nil, errFileTooLarge
	}
	return data, nil
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return &Error{Code: CodeCancelled, Err: ctx.Err()}
	default:
		return nil
	}
}
