package db

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

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
