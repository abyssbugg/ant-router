package controltoken

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type tokenWriteHooks struct {
	syncDirectory func(string) error
}

func writeTokenAtomic(path string, data []byte, mode fs.FileMode) error {
	return writeTokenAtomicWithHooks(path, data, mode, nil)
}

func writeTokenAtomicWithHooks(path string, data []byte, mode fs.FileMode, hooks *tokenWriteHooks) (err error) {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".control.token.tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	closed := false
	committed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode.Perm()); err != nil {
		return err
	}
	written, err := io.Copy(temporary, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if written != int64(len(data)) {
		return io.ErrShortWrite
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	if err := replaceTokenFile(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	syncOperation := syncTokenDirectory
	if hooks != nil && hooks.syncDirectory != nil {
		syncOperation = hooks.syncDirectory
	}
	if err := syncOperation(directory); err != nil {
		return &Error{Code: CodeDurabilityUncertain, Path: "token_file", Err: err}
	}
	return nil
}
