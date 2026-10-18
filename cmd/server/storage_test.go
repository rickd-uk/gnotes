//go:build cgo && (sqlite_fts5 || fts5)

package main

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"gnotes/internal/db"
	"gnotes/internal/storage"
)

func encryptedFixture(t *testing.T) (testLogin, string, string) {
	t.Helper()
	_, _, login, _ := recoveryFixture(t)
	dir := t.TempDir()
	keyFile, encrypted := filepath.Join(dir, "database.key"), filepath.Join(dir, "encrypted.db")
	if err := storage.GenerateKey(keyFile); err != nil {
		t.Fatal(err)
	}
	key, err := storage.ReadKey(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	// Discover the existing fixture file without coupling to fixture internals.
	var seq int
	var name, plain string
	if err := db.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &plain); err != nil {
		t.Fatal(err)
	}
	if err := storage.Copy(context.Background(), plain, nil, encrypted, key); err != nil {
		t.Fatal(err)
	}
	db.DB.Close()
	if err := db.InitDBWithKey(encrypted, key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close(); clear(key) })
	return login, encrypted, keyFile
}

func TestEncryptedAccountNoteFlows(t *testing.T) {
	login, _, _ := encryptedFixture(t)
	created := authenticatedRequest(t, protect(createNoteHandler, true), "POST", "/api/notes/create", `{"title":"PrivateOrchid","content":"Secret body"}`, login, true)
	if created.Code != http.StatusOK && created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	search := authenticatedRequest(t, protect(pagedSearchNotesHandler, false), "GET", "/api/notes/search-page?q=PrivateOrchid", "", login, false)
	if search.Code != http.StatusOK || !bytes.Contains(search.Body.Bytes(), []byte("Secret body")) {
		t.Fatalf("search: %d %s", search.Code, search.Body.String())
	}
	update := authenticatedRequest(t, protect(updateNoteHandler, true), "PUT", "/api/notes/update?id=1", `{"title":"Changed","content":"Edited body"}`, login, true)
	if update.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", update.Code, update.Body.String())
	}
	lock := authenticatedRequest(t, protect(lockNoteHandler, true), "PUT", "/api/notes/lock?id=1", `{"locked":true}`, login, true)
	if lock.Code != http.StatusOK {
		t.Fatalf("read only: %d %s", lock.Code, lock.Body.String())
	}
	backup := authenticatedRequest(t, protect(backupDownloadHandler, true), "POST", "/api/backups/download", `{"password":"original password","kind":"personal-backup"}`, login, true)
	if backup.Code != http.StatusOK || bytes.Contains(backup.Body.Bytes(), []byte("Edited body")) {
		t.Fatalf("personal encrypted backup: %d", backup.Code)
	}
	var createdAt time.Time
	if err := db.DB.QueryRow("SELECT created_at FROM notes WHERE id=1").Scan(&createdAt); err != nil || createdAt.Year() < 2026 {
		t.Fatalf("encrypted timestamp: %v %v", createdAt, err)
	}
}

func TestDatabaseKeyAndCommands(t *testing.T) {
	t.Setenv("GNOTES_DATABASE_KEY_FILE", "")
	t.Setenv("GNOTES_REQUIRE_ENCRYPTION", "1")
	if _, err := databaseKey(); err == nil {
		t.Fatal("required encryption started without a key")
	}
	login, encrypted, keyFile := encryptedFixture(t)
	_ = login
	t.Setenv("GNOTES_DATABASE_KEY_FILE", keyFile)
	key, err := databaseKey()
	if err != nil || len(key) != 32 {
		t.Fatal("configured key unavailable", err)
	}
	clear(key)
	var out bytes.Buffer
	for _, args := range [][]string{
		{"verify", "--source", encrypted},
		{"snapshot", "--source", encrypted, "--output", filepath.Join(t.TempDir(), "backup.db")},
	} {
		if err := runDatabaseCommand(context.Background(), args, &out); err != nil {
			t.Fatal(args[0], err)
		}
	}
	for _, args := range [][]string{
		{"verify", "--source", encrypted, "--key-file", ""},
		{"snapshot", "--source", encrypted},
		{"encrypt", "--source", encrypted, "--output", filepath.Join(t.TempDir(), "invalid.db")},
		{"verify", "--source", encrypted, "--output", "unexpected.db"},
	} {
		if err := runDatabaseCommand(context.Background(), args, &out); err == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
}
