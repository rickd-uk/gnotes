//go:build cgo && (sqlite_fts5 || fts5)

package storage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gnotes/internal/db"
	"gnotes/internal/storage"
)

func TestEncryptedMigrationSnapshotAndRecovery(t *testing.T) {
	dir := t.TempDir()
	plain, encrypted, snapshot := filepath.Join(dir, "plain.db"), filepath.Join(dir, "encrypted.db"), filepath.Join(dir, "snapshot.db")
	key := make([]byte, 32)
	rand.Read(key)
	db.InitDB(plain)
	stamp := time.Date(2026, 10, 18, 12, 34, 56, 123456789, time.UTC)
	marker := "PrivateOrchidSignal-very-secret-notes"
	for _, query := range []string{
		"INSERT INTO users(id,username,password_hash,role,created_at) VALUES(1,'rick',X'010203','admin',CURRENT_TIMESTAMP)",
		"INSERT INTO settings(key,value) VALUES('preserve-me','original')",
		"PRAGMA user_version=73; PRAGMA application_id=12345;",
	} {
		if _, err := db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec("INSERT INTO notes(user_id,title,content,created_at,locked,tags) VALUES(1,?,?,?,1,'[\"private\"]')", marker, marker, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO drafts(user_id,title,content,updated_at) VALUES(1,?,?,?)", marker, marker, stamp); err != nil {
		t.Fatal(err)
	}
	// Copy while the original connection is open: include committed WAL data.
	if err := storage.Copy(context.Background(), plain, nil, encrypted, key); err != nil {
		t.Fatal(err)
	}
	db.DB.Close()
	if err := db.InitDBWithKey(encrypted, key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	var title, content string
	var created time.Time
	var locked bool
	if err := db.DB.QueryRow("SELECT title,content,created_at,locked FROM notes WHERE id=1").Scan(&title, &content, &created, &locked); err != nil || title != marker || content != marker || !locked || !created.Equal(stamp) {
		t.Fatalf("migration changed note: %q %q %v %v %v", title, content, created, locked, err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM notes_fts WHERE notes_fts MATCH 'OrchidSignal'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("encrypted search: count=%d err=%v", count, err)
	}
	// Replacing an idle connection must reapply the key and security settings.
	db.DB.SetMaxIdleConns(0)
	for i := 0; i < 2; i++ {
		if err := db.DB.QueryRow("SELECT count(*) FROM drafts WHERE content=?", marker).Scan(&count); err != nil || count != 1 {
			t.Fatalf("reconnect: %d %v", count, err)
		}
	}
	db.DB.SetMaxIdleConns(1)
	if _, err := db.DB.Exec("INSERT INTO notes(user_id,title,content,created_at) VALUES(1,?,?,?)", marker, marker+"-WAL", stamp); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(encrypted + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(marker)) || bytes.HasPrefix(data, []byte("SQLite format 3")) {
			t.Fatalf("plaintext leaked to encrypted database%s", suffix)
		}
	}
	if err := storage.Copy(context.Background(), encrypted, key, snapshot, key); err != nil {
		t.Fatal(err)
	}
	check, err := storage.Open(snapshot, key, "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	if err := storage.Verify(check); err != nil {
		t.Fatal(err)
	}
	if err := check.QueryRow("SELECT count(*) FROM notes").Scan(&count); err != nil || count != 2 {
		t.Fatalf("snapshot omitted WAL data: %d %v", count, err)
	}
	for pragma, want := range map[string]int{"user_version": 73, "application_id": 12345} {
		if err := check.QueryRow("PRAGMA " + pragma).Scan(&count); err != nil || count != want {
			t.Fatalf("%s=%d err=%v", pragma, count, err)
		}
	}
	if other, err := storage.Open(snapshot, nil, "ro"); err == nil {
		other.Close()
		t.Fatal("encrypted backup accepted without a key")
	}
	wrong := make([]byte, 32)
	rand.Read(wrong)
	if other, err := storage.Open(snapshot, wrong, "ro"); err == nil {
		other.Close()
		t.Fatal("encrypted backup accepted with wrong key")
	}
	before, _ := os.ReadFile(snapshot)
	if err := storage.Copy(context.Background(), encrypted, key, snapshot, key); err == nil {
		t.Fatal("snapshot overwritten")
	}
	after, _ := os.ReadFile(snapshot)
	if !bytes.Equal(before, after) {
		t.Fatal("failed copy modified existing backup")
	}
	// A modified authenticated page must not be accepted as a valid backup.
	before[len(before)-50] ^= 1
	tampered := filepath.Join(dir, "tampered.db")
	os.WriteFile(tampered, before, 0600)
	bad, err := storage.Open(tampered, key, "ro")
	if err == nil {
		err = storage.Verify(bad)
		bad.Close()
	}
	if err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestKeyFilesAndFailedMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "database.key")
	if err := storage.GenerateKey(path); err != nil {
		t.Fatal(err)
	}
	key, err := storage.ReadKey(path)
	if err != nil || len(key) != 32 {
		t.Fatal("cannot read generated key", err)
	}
	if err := storage.GenerateKey(path); err == nil {
		t.Fatal("existing key overwritten")
	}
	alias := filepath.Join(dir, "alias.key")
	os.Symlink(path, alias)
	if _, err := storage.ReadKey(alias); err == nil {
		t.Fatal("symlink key accepted")
	}
	os.Chmod(path, 0644)
	if _, err := storage.ReadKey(path); err == nil {
		t.Fatal("world-readable key accepted")
	}
	os.Chmod(path, 0600)
	os.WriteFile(path, []byte("a short passphrase"), 0600)
	if _, err := storage.ReadKey(path); err == nil {
		t.Fatal("passphrase treated as a random key")
	}
	plain := filepath.Join(dir, "incomplete.db")
	database, err := storage.Open(plain, nil, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	database.Exec("CREATE TABLE notes(content TEXT)")
	database.Close()
	destination := filepath.Join(dir, "incomplete-encrypted.db")
	if err := storage.Copy(context.Background(), plain, nil, destination, key); err == nil {
		t.Fatal("incomplete database published")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("failed snapshot left a published file")
	}
	partials, _ := filepath.Glob(filepath.Join(dir, ".gnotes-encrypted-*"))
	if len(partials) != 0 {
		t.Fatal("failed snapshot left temporary files", partials)
	}
}
