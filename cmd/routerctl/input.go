package main

import (
	"errors"
	"io"
	"os"

	"github.com/abyssbugg/ant-router/internal/config"
)

var (
	errConfigurationTooLarge = errors.New("configuration exceeds 1 MiB")
	errConfigurationRead     = errors.New("cannot read configuration")
)

func readConfiguration(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		data, err := readBoundedConfiguration(stdin)
		if err == nil || errors.Is(err, errConfigurationTooLarge) {
			return data, err
		}
		return nil, errors.Join(errConfigurationRead, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.Join(errConfigurationRead, err)
	}
	data, readErr := readBoundedConfiguration(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, errors.Join(errConfigurationRead, readErr)
	}
	if closeErr != nil {
		return nil, errors.Join(errConfigurationRead, closeErr)
	}
	return data, nil
}

func readBoundedConfiguration(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(config.MaxSourceBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > config.MaxSourceBytes {
		return nil, errConfigurationTooLarge
	}
	return data, nil
}
