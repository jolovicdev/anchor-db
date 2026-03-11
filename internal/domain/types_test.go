package domain

import (
	"crypto/rand"
	"errors"
	"testing"
)

func TestNewIDPanicsWhenRandomReaderFails(t *testing.T) {
	originalReader := rand.Reader
	rand.Reader = failingReader{}
	defer func() {
		rand.Reader = originalReader
	}()

	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic when crypto rand fails")
		}
	}()

	_ = NewID("anchor")
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("boom")
}
