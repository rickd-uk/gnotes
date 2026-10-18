// Package storage owns database encryption and operational database access.
// Keys are supplied separately from database filenames and never logged.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

// Open opens a database without changing its application schema. A nonempty
// key requires SQLCipher; an unreadable database never falls back to plaintext.
func Open(path string, key []byte, mode string) (*sql.DB, error) {
	if mode != "ro" && mode != "rw" && mode != "rwc" {
		return nil, errors.New("invalid database open mode")
	}
	if len(key) != 0 && len(key) != 32 {
		return nil, errors.New("database key must contain 32 random bytes")
	}
	var database *sql.DB
	var err error
	if len(key) != 0 {
		if path == ":memory:" || strings.ContainsAny(path, "?#") || strings.HasPrefix(path, "file:") {
			return nil, errors.New("encrypted database requires a filesystem path without URI parameters")
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, errors.New("invalid database path")
		}
		uri := &url.URL{Scheme: "file", Path: absolute}
		q := url.Values{"mode": {mode}, "_busy_timeout": {"5000"}, "_foreign_keys": {"on"}, "_synchronous": {"FULL"}, "_secure_delete": {"on"}}
		uri.RawQuery = q.Encode()
		if mode == "rwc" {
			file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if createErr == nil {
				file.Close()
			} else if !errors.Is(createErr, os.ErrExist) {
				return nil, errors.New("cannot create encrypted database")
			}
		}
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, errors.New("encrypted database must be a regular file, not a symlink")
		}
		database, err = openEncrypted(uri.String(), key)
	} else {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		q := url.Values{"_pragma": {"busy_timeout(5000)", "foreign_keys(ON)", "synchronous(FULL)", "temp_store(MEMORY)", "mmap_size(0)"}}
		if path != ":memory:" {
			q.Set("mode", mode)
		}
		database, err = sql.Open("sqlite", path+separator+q.Encode())
	}
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	ok := false
	defer func() {
		if !ok {
			database.Close()
		}
	}()
	if err := database.PingContext(context.Background()); err != nil {
		return nil, errors.New("cannot open database: wrong/missing key, damaged file or unavailable storage")
	}
	if len(key) > 0 {
		var version string
		if err := database.QueryRow("PRAGMA cipher_version").Scan(&version); err != nil || !strings.HasPrefix(version, "4.") {
			return nil, errors.New("SQLCipher 4 encryption support is required")
		}
	}
	var integrity string
	if err := database.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		return nil, errors.New("database integrity/key verification failed")
	}
	if mode != "ro" && path != ":memory:" {
		var journal string
		if err := database.QueryRow("PRAGMA journal_mode=WAL").Scan(&journal); err != nil || journal != "wal" {
			return nil, errors.New("cannot enable database WAL mode")
		}
	}
	ok = true
	return database, nil
}

// Verify checks every page and the minimum schema required by restore tools.
// It deliberately does not run application migrations.
func Verify(database *sql.DB) error {
	// Plain SQLite returns no cipher_version row. For SQLCipher, independently
	// verify page authentication as well as SQLite's logical structure.
	var version string
	if err := database.QueryRow("PRAGMA cipher_version").Scan(&version); err == nil && version != "" {
		pages, err := database.Query("PRAGMA cipher_integrity_check")
		if err != nil {
			return errors.New("encrypted page authentication failed")
		}
		invalid := pages.Next()
		err = pages.Err()
		pages.Close()
		if invalid || err != nil {
			return errors.New("encrypted page authentication failed")
		}
	}
	rows, err := database.Query("PRAGMA integrity_check")
	if err != nil {
		return errors.New("database integrity verification failed")
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil || result != "ok" {
			return errors.New("database integrity verification failed")
		}
		count++
	}
	if rows.Err() != nil || count != 1 {
		return errors.New("database integrity verification failed")
	}
	rows.Close()
	for table, required := range map[string][]string{
		"users": {"username", "password_hash"}, "notes": {"user_id", "content"},
		"sessions": {"token_hash"}, "drafts": {"user_id"}, "settings": {},
		"rate_limits": {}, "login_cooldowns": {}, "signup_events": {},
		"invitations": {}, "security_daily": {},
	} {
		columns, err := database.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return fmt.Errorf("cannot verify table %s", table)
		}
		found := map[string]bool{}
		for columns.Next() {
			var cid, notnull, pk int
			var name, kind string
			var defaultValue any
			if err := columns.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
				columns.Close()
				return fmt.Errorf("cannot verify table %s", table)
			}
			found[name] = true
		}
		err = columns.Err()
		columns.Close()
		if err != nil || len(found) == 0 {
			return fmt.Errorf("missing or unreadable database table: %s", table)
		}
		for _, column := range required {
			if !found[column] {
				return fmt.Errorf("missing database column: %s.%s", table, column)
			}
		}
	}
	return nil
}
