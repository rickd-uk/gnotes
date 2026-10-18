//go:build !cgo

package storage

import (
	"path/filepath"
	"testing"
)

func TestEncryptionUnavailableFailsClosed(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "encrypted.db"), make([]byte, 32), "rwc")
	if database != nil {
		database.Close()
	}
	if err == nil {
		t.Fatal("non-SQLCipher build silently accepted an encryption key")
	}
}
