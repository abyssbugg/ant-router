package controltoken

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/abyssbugg/ant-router/internal/controltransport"
)

var errTokenExists = errors.New("token already exists")

// Store persists one private control token per named instance.
// Its mutex intentionally serializes token reads, creation, and rotation.
type Store struct {
	mu        sync.Mutex
	stateRoot string
	random    io.Reader
	writeFile func(string, []byte, fs.FileMode) error
}

// NewStore creates a token store rooted at an absolute private directory.
func NewStore(stateRoot string) (*Store, error) {
	stateRoot = strings.TrimSpace(stateRoot)
	if filepath.Clean(stateRoot) == "." || stateRoot == "" {
		return nil, &Error{Code: CodeStoreUnavailable, Path: "state_root", Err: errors.New("state root is required")}
	}
	absolute, err := filepath.Abs(stateRoot)
	if err != nil {
		return nil, &Error{Code: CodeStoreUnavailable, Path: "state_root", Err: errors.New("cannot resolve state root")}
	}
	if err := ensurePrivateDirectory(filepath.Clean(absolute)); err != nil {
		return nil, err
	}
	return &Store{stateRoot: filepath.Clean(absolute), random: rand.Reader, writeFile: writeTokenAtomic}, nil
}

// Load reads and validates an existing instance token.
func (s *Store) Load(instance controltransport.InstanceName) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(instance)
}

// LoadOrCreate loads an existing token or creates one exactly once.
func (s *Store) LoadOrCreate(instance controltransport.InstanceName) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths, err := s.ensureInstance(instance)
	if err != nil {
		return Token{}, err
	}
	if token, err := s.loadPath(paths.TokenPath); err == nil {
		return token, nil
	} else if code, _ := CodeOf(err); code != CodeTokenNotFound {
		return Token{}, err
	}
	token, err := Generate(s.random)
	if err != nil {
		return Token{}, err
	}
	if err := createTokenExclusive(paths.TokenPath, []byte(token.String())); err != nil {
		if errors.Is(err, errTokenExists) {
			return s.loadWinner(paths.TokenPath)
		}
		return Token{}, err
	}
	return token, nil
}

// Rotate atomically persists and returns a new token for an existing instance.
func (s *Store) Rotate(instance controltransport.InstanceName) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths, err := controltransport.ResolveInstancePaths(s.stateRoot, instance)
	if err != nil {
		return Token{}, &Error{Code: CodeStoreUnavailable, Path: "instance", Err: err}
	}
	if _, err := s.loadPath(paths.TokenPath); err != nil {
		return Token{}, err
	}
	token, err := Generate(s.random)
	if err != nil {
		return Token{}, err
	}
	if err := s.writeFile(paths.TokenPath, []byte(token.String()), 0o600); err != nil {
		if code, _ := CodeOf(err); code == CodeDurabilityUncertain {
			return Token{}, err
		}
		return Token{}, &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	return token, nil
}

func (s *Store) load(instance controltransport.InstanceName) (Token, error) {
	paths, err := controltransport.ResolveInstancePaths(s.stateRoot, instance)
	if err != nil {
		return Token{}, &Error{Code: CodeStoreUnavailable, Path: "instance", Err: err}
	}
	return s.loadPath(paths.TokenPath)
}

func (s *Store) loadPath(path string) (Token, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, &Error{Code: CodeTokenNotFound, Path: "token_file"}
		}
		return Token{}, &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	if !info.Mode().IsRegular() || !privateTokenFile(path, info.Mode()) {
		return Token{}, &Error{Code: CodeTokenCorrupt, Path: "token_file", Err: errors.New("token file is invalid")}
	}
	file, err := os.Open(path)
	if err != nil {
		return Token{}, &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	data, readErr := io.ReadAll(io.LimitReader(file, EncodedTokenLength+1))
	closeErr := file.Close()
	if readErr != nil {
		return Token{}, &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: readErr}
	}
	if closeErr != nil {
		return Token{}, &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: closeErr}
	}
	if len(data) != EncodedTokenLength {
		return Token{}, &Error{Code: CodeTokenCorrupt, Path: "token_file", Err: errors.New("token file is invalid")}
	}
	token, err := Parse(string(data))
	if err != nil {
		return Token{}, &Error{Code: CodeTokenCorrupt, Path: "token_file", Err: errors.New("token file is invalid")}
	}
	return token, nil
}

func (s *Store) ensureInstance(instance controltransport.InstanceName) (controltransport.InstancePaths, error) {
	paths, err := controltransport.ResolveInstancePaths(s.stateRoot, instance)
	if err != nil {
		return controltransport.InstancePaths{}, &Error{Code: CodeStoreUnavailable, Path: "instance", Err: err}
	}
	if err := ensurePrivateDirectory(filepath.Dir(paths.StateDir)); err != nil {
		return controltransport.InstancePaths{}, err
	}
	if err := ensurePrivateDirectory(paths.StateDir); err != nil {
		return controltransport.InstancePaths{}, err
	}
	return paths, nil
}

func (s *Store) loadWinner(path string) (Token, error) {
	return s.loadPath(path)
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: "state_directory", Err: err}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: "state_directory", Err: err}
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: CodePermissionDenied, Path: "state_directory", Err: errors.New("state directory is invalid")}
	}
	if err := secureTokenDirectory(path); err != nil {
		return &Error{Code: CodePermissionDenied, Path: "state_directory", Err: err}
	}
	return nil
}

func createTokenExclusive(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".control.token.create-*")
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	written, err := io.Copy(temporary, bytes.NewReader(data))
	if err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	if written != int64(len(data)) {
		return &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: io.ErrShortWrite}
	}
	if err := temporary.Sync(); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	if err := temporary.Close(); err != nil {
		return &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	closed = true
	if err := installTokenFile(temporaryPath, path); err != nil {
		if errors.Is(err, errTokenExists) {
			return errTokenExists
		}
		return &Error{Code: CodeStoreUnavailable, Path: "token_file", Err: err}
	}
	if err := syncTokenDirectory(directory); err != nil {
		return &Error{Code: CodeDurabilityUncertain, Path: "token_file", Err: err}
	}
	return nil
}
