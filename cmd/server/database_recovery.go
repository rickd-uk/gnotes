package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gnotes/internal/storage"
)

// Test recovery into disposable encrypted files. The supplied database/archive
// and key are only read; no service, schema migration, or production path is used.
func testDatabaseRecovery(ctx context.Context, source string, key []byte, output io.Writer) error {
	if len(key) != 32 {
		return errors.New("recovery-test requires the matching --key-file")
	}
	directory, err := os.MkdirTemp("", "gnotes-recovery-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	input, err := os.Open(source)
	if err != nil {
		return errors.New("cannot read backup")
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("backup must be a regular file")
	}
	header := make([]byte, 16)
	if _, err := io.ReadFull(input, header); err != nil {
		return errors.New("backup is empty or truncated")
	}
	if string(header) == "SQLite format 3\x00" {
		return errors.New("recovery-test requires an encrypted database backup")
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return err
	}
	var reader io.Reader = input
	if header[0] == 0x1f && header[1] == 0x8b {
		archive, err := gzip.NewReader(input)
		if err != nil {
			return errors.New("invalid gzip backup")
		}
		defer archive.Close()
		reader = archive
	}
	buffered := bufio.NewReader(reader)
	signature, err := buffered.Peek(16)
	if err != nil {
		return errors.New("backup is empty or truncated")
	}
	if string(signature) == "SQLite format 3\x00" {
		return errors.New("recovery-test requires an encrypted database backup")
	}
	reader = buffered
	staged := filepath.Join(directory, "backup.db")
	file, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return errors.New("could not stage complete backup")
	}
	// Verify staged bytes with no key and a fresh wrong key before recovery.
	accepts := func(candidateKey []byte) bool {
		database, err := storage.Open(staged, candidateKey, "ro")
		if err != nil {
			return false
		}
		defer database.Close()
		return storage.Verify(database) == nil
	}
	if accepts(nil) {
		return errors.New("backup is readable without its database key")
	}
	wrong := make([]byte, 32)
	if _, err := rand.Read(wrong); err != nil {
		return err
	}
	defer clear(wrong)
	if accepts(wrong) {
		return errors.New("backup accepted an incorrect key")
	}
	recovered := filepath.Join(directory, "recovered.db")
	// Copy validates source integrity and compares all stored rows with the
	// encrypted destination in one consistent snapshot.
	if err := storage.Copy(ctx, staged, key, recovered, key); err != nil {
		return fmt.Errorf("recovery failed: %w", err)
	}
	database, err := storage.Open(recovered, key, "rw")
	if err != nil {
		return err
	}
	// Exercise committed writes, close and reopen as on a replacement host.
	probeTable := "gnotes_recovery_probe_" + hex.EncodeToString(wrong[:8])
	_, err = database.ExecContext(ctx, `CREATE TABLE `+probeTable+`(value TEXT NOT NULL); INSERT INTO `+probeTable+` VALUES('recovery verified')`)
	if err != nil {
		database.Close()
		return fmt.Errorf("restored database is not writable: %w", err)
	}
	if err := database.Close(); err != nil {
		return err
	}
	database, err = storage.Open(recovered, key, "rw")
	if err != nil {
		return err
	}
	defer database.Close()
	var probe string
	if err := database.QueryRowContext(ctx, `SELECT value FROM `+probeTable).Scan(&probe); err != nil || probe != "recovery verified" {
		return errors.New("restored database did not retain writes after reopening")
	}
	if _, err := database.ExecContext(ctx, `DROP TABLE `+probeTable); err != nil {
		return err
	}
	if err := storage.Verify(database); err != nil {
		return err
	}
	var users, notes int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		return err
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&notes); err != nil {
		return err
	}
	fmt.Fprintf(output, "Recovery test passed: %d accounts, %d notes.\n", users, notes)
	fmt.Fprintln(output, "Missing/wrong keys rejected; encrypted restore, all stored rows, integrity, committed writes and reopening verified.")
	fmt.Fprintln(output, "Source backup and key unchanged. Temporary encrypted recovery files removed on exit. No running service was changed.")
	return nil
}
