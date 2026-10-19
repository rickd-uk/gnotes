//go:build cgo && (sqlite_fts5 || fts5)

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gnotes/internal/storage"
)

func TestDatabaseRecoveryBackupAndOfflineKey(t *testing.T) {
	t.Setenv("GNOTES_VOLATILE_KEY", "")
	_, encrypted, keyFile := encryptedFixture(t)
	original, err := os.ReadFile(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "backup.db.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gzipFile := gzip.NewWriter(file)
	gzipFile.Write(original)
	gzipFile.Close()
	file.Close()
	wrong := filepath.Join(dir, "wrong.key")
	if err := storage.GenerateKey(wrong); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{encrypted, archive} {
		var output bytes.Buffer
		if err := runDatabaseCommand(context.Background(), []string{"recovery-test", "--source", source, "--key-file", keyFile}, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "Recovery test passed: 2 accounts") || !strings.Contains(output.String(), "Missing/wrong keys rejected") {
			t.Fatal(output.String())
		}
		for _, key := range []string{"", wrong} {
			if err := runDatabaseCommand(context.Background(), []string{"recovery-test", "--source", source, "--key-file", key}, &bytes.Buffer{}); err == nil {
				t.Fatal("incorrect key accepted")
			}
		}
	}
	after, _ := os.ReadFile(encrypted)
	keyAfter, _ := os.ReadFile(keyFile)
	if sha256.Sum256(original) != sha256.Sum256(after) || !bytes.Equal(keyBefore, keyAfter) {
		t.Fatal("recovery test modified source or key")
	}
	plaintext := filepath.Join(dir, "plain.db")
	os.WriteFile(plaintext, append([]byte("SQLite format 3\x00"), make([]byte, 100)...), 0600)
	if err := runDatabaseCommand(context.Background(), []string{"recovery-test", "--source", plaintext, "--key-file", keyFile}, &bytes.Buffer{}); err == nil {
		t.Fatal("plaintext accepted")
	}
	// A corrupt archive must fail even if the decompressed prefix is valid.
	data, _ := os.ReadFile(archive)
	os.WriteFile(filepath.Join(dir, "truncated.gz"), data[:len(data)-5], 0600)
	if err := runDatabaseCommand(context.Background(), []string{"recovery-test", "--source", filepath.Join(dir, "truncated.gz"), "--key-file", keyFile}, &bytes.Buffer{}); err == nil {
		t.Fatal("truncated archive accepted")
	}
}
