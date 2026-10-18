package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// Copy creates a new verified SQLCipher database without modifying the source.
// It is used both for migration and consistent online encrypted snapshots.
// Publishing uses a no-overwrite hard link, after syncing the candidate file.
func Copy(ctx context.Context, source string, sourceKey []byte, output string, outputKey []byte) error {
	if len(outputKey) != 32 || (len(sourceKey) != 0 && len(sourceKey) != 32) {
		return errors.New("encrypted copies require a 32-byte destination key")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return errors.New("invalid source path")
	}
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("source must be a regular database file")
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		return errors.New("output already exists or cannot be inspected")
	}
	uri := &url.URL{Scheme: "file", Path: source, RawQuery: "mode=ro&_busy_timeout=10000&_synchronous=FULL"}
	database, err := openEncrypted(uri.String(), sourceKey)
	if err != nil {
		return err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if err := database.PingContext(ctx); err != nil {
		return errors.New("cannot read source database: check its key and integrity")
	}
	// Do not trust an unencrypted SQLite build to understand KEY or export.
	var version string
	if err := database.QueryRowContext(ctx, "PRAGMA cipher_version").Scan(&version); err != nil || version == "" {
		return errors.New("SQLCipher encryption support is required")
	}
	candidate, err := os.CreateTemp(filepath.Dir(output), ".gnotes-encrypted-*.db")
	if err != nil {
		return errors.New("cannot create encrypted copy")
	}
	path := candidate.Name()
	candidate.Close()
	defer func() {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			os.Remove(path + suffix)
		}
	}()
	conn, err := database.Conn(ctx)
	if err != nil {
		return errors.New("cannot open source connection")
	}
	defer conn.Close()
	// The raw-key SQL literal is bound as data, never concatenated into SQL.
	// Destination pages (including its rollback journal) are encrypted from
	// their first write. A new random salt is created for each copy.
	keyLiteral := "x'" + hex.EncodeToString(outputKey) + "'"
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS protected KEY ?", path, keyLiteral); err != nil {
		return errors.New("cannot attach encrypted destination")
	}
	defer conn.ExecContext(context.Background(), "DETACH DATABASE protected")
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return errors.New("cannot begin consistent database snapshot")
	}
	defer tx.Rollback()
	// Pin a read snapshot before reading metadata or copying any tables.
	var tables int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&tables); err != nil {
		return errors.New("cannot read database snapshot")
	}
	// Verify the source inside that same snapshot before exporting it. A
	// successful logical export must not hide damage in the source database.
	rows, err := tx.QueryContext(ctx, "PRAGMA main.integrity_check")
	if err != nil {
		return errors.New("source database integrity verification failed")
	}
	checks := 0
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil || result != "ok" {
			rows.Close()
			return errors.New("source database integrity verification failed")
		}
		checks++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || checks != 1 {
		return errors.New("source database integrity verification failed")
	}
	var userVersion, applicationID int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		return errors.New("cannot read database version")
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); err != nil {
		return errors.New("cannot read database identifier")
	}
	if _, err := tx.ExecContext(ctx, "SELECT sqlcipher_export('protected')"); err != nil {
		return errors.New("encrypted database export failed")
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA protected.user_version=%d; PRAGMA protected.application_id=%d;", userVersion, applicationID)); err != nil {
		return errors.New("cannot preserve database version")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("cannot commit encrypted database copy")
	}
	if _, err := conn.ExecContext(ctx, "DETACH DATABASE protected"); err != nil {
		return errors.New("cannot close encrypted destination")
	}
	check, err := Open(path, outputKey, "ro")
	if err != nil {
		return err
	}
	err = Verify(check)
	check.Close()
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return errors.New("cannot sync encrypted database copy")
	}
	err = file.Sync()
	file.Close()
	if err != nil {
		return errors.New("cannot sync encrypted database copy")
	}
	if err := os.Link(path, output); err != nil {
		return errors.New("cannot publish encrypted copy; output must not exist")
	}
	dir, err := os.Open(filepath.Dir(output))
	if err != nil {
		return errors.New("copy published but directory could not be opened for sync")
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return errors.New("copy published but directory sync failed")
	}
	return nil
}
