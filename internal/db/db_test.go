package db

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSignupEventMigrationPreservesSecurityHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signup-event-migration.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL COLLATE NOCASE UNIQUE,
			password_hash BLOB NOT NULL,
			role TEXT NOT NULL DEFAULT 'user',
			active INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL
		);
		CREATE TABLE signup_events (
			user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			ip_hash TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);
		INSERT INTO users (id, username, password_hash, role, active, created_at)
		VALUES (1, 'alice', X'00', 'user', 1, CURRENT_TIMESTAMP);
		INSERT INTO signup_events (user_id, ip_hash, created_at)
		VALUES (1, 'hashed-ip', CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	InitDB(path)
	t.Cleanup(func() { DB.Close() })
	rows, err := DB.Query("PRAGMA foreign_key_list(signup_events)")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("signup_events still cascades when a user is deleted")
	}
	rows.Close()
	if _, err := DB.Exec("DELETE FROM users WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM signup_events WHERE user_id = 1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("signup security history count = %d, want 1", count)
	}
}

func TestInitDBConfiguresSQLiteForReliability(t *testing.T) {
	InitDB(filepath.Join(t.TempDir(), "reliability.db"))
	t.Cleanup(func() { DB.Close() })

	checks := []struct {
		query string
		want  string
	}{
		{"PRAGMA journal_mode", "wal"},
		{"PRAGMA synchronous", "2"},
		{"PRAGMA busy_timeout", "5000"},
		{"PRAGMA foreign_keys", "1"},
		{"PRAGMA quick_check", "ok"},
	}
	for _, check := range checks {
		var got string
		if err := DB.QueryRow(check.query).Scan(&got); err != nil {
			t.Fatalf("%s: %v", check.query, err)
		}
		if got != check.want {
			t.Fatalf("%s = %q, want %q", check.query, got, check.want)
		}
	}
	if got := DB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
}

func TestConcurrentWritesAreSerialized(t *testing.T) {
	InitDB(filepath.Join(t.TempDir(), "concurrent.db"))
	t.Cleanup(func() { DB.Close() })

	result, err := DB.Exec(
		"INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
		"writer", []byte("not-used-in-this-test"), "user", time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	const writes = 24
	errors := make(chan error, writes)
	var wait sync.WaitGroup
	for i := 0; i < writes; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := DB.Exec(
				"INSERT INTO notes (user_id, title, content, created_at) VALUES (?, ?, ?, ?)",
				userID, "concurrent", "write", time.Now(),
			)
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}

	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id = ?", userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != writes {
		t.Fatalf("stored %d concurrent writes, want %d", count, writes)
	}
}
