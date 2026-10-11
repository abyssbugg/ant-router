package anthropic

import (
	"errors"
	"io"
	"testing"
)

type zeroNilBody struct{}

func (zeroNilBody) Read([]byte) (int, error) { return 0, nil }
func (zeroNilBody) Close() error             { return nil }

func TestBoundedReadCloserRejectsZeroNilRead(t *testing.T) {
	reader := &boundedReadCloser{body: zeroNilBody{}, remaining: 64}
	buffer := make([]byte, 8)
	n, err := reader.Read(buffer)
	if n != 0 || !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("Read() = (%d, %v), want (0, %v)", n, err, io.ErrNoProgress)
	}
}

func TestBoundedReadCloserRejectsZeroNilProbeRead(t *testing.T) {
	reader := &boundedReadCloser{body: zeroNilBody{}, remaining: 0}
	buffer := make([]byte, 8)
	n, err := reader.Read(buffer)
	if n != 0 || !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("Read() = (%d, %v), want (0, %v)", n, err, io.ErrNoProgress)
	}
}
