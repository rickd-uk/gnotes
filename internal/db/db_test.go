package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConnectionPragmasSurviveReconnect(t *testing.T) {
	for _, suffix := range []string{"", "?cache=private&_pragma=foreign_keys(OFF)"} {
		t.Run(suffix, func(t *testing.T) {
			InitDB(filepath.Join(t.TempDir(), "reconnect.db") + suffix)
			t.Cleanup(func() { DB.Close() })
			// Closing the idle connection models the pool replacing a canceled
			// transaction's connection. Settings must apply to its replacement.
			DB.SetMaxIdleConns(0)
			conn, err := DB.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			for pragma, want := range map[string]int{"foreign_keys": 1, "synchronous": 2, "busy_timeout": 5000} {
				var got int
				if err := conn.QueryRowContext(context.Background(), "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
					t.Errorf("replacement connection %s: got=%d want=%d err=%v", pragma, got, want, err)
				}
			}
			var journal string
			if err := conn.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
				t.Fatalf("replacement connection journal mode=%q err=%v", journal, err)
			}
		})
	}
}

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

func TestInitDBInvalidatesOldRenderedContentCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "render-cache.db")
	InitDB(path)
	if _, err := DB.Exec(
		"INSERT INTO notes (title, content, rendered_content, created_at) VALUES (?, ?, ?, ?)",
		"Code", "```c\\nint main(void) {}\\n```", "<pre>old rendering</pre>", time.Now(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := DB.Exec(
		"UPDATE settings SET value = '1' WHERE key = 'rendered_content_cache_version'",
	); err != nil {
		t.Fatal(err)
	}
	if err := DB.Close(); err != nil {
		t.Fatal(err)
	}

	InitDB(path)
	t.Cleanup(func() { DB.Close() })
	var rendered sql.NullString
	if err := DB.QueryRow("SELECT rendered_content FROM notes LIMIT 1").Scan(&rendered); err != nil {
		t.Fatal(err)
	}
	if rendered.Valid {
		t.Fatalf("old rendered content cache was retained: %q", rendered.String)
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

func TestNotesFTSStaysSynchronized(t *testing.T) {
	InitDB(filepath.Join(t.TempDir(), "fts.db"))
	t.Cleanup(func() { DB.Close() })

	result, err := DB.Exec(
		"INSERT INTO notes (title, content, rendered_content, created_at) VALUES ('Project', 'alpha marker', '<p>alpha marker</p>', ?)",
		time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	assertFTSCount(t, `notes_fts MATCH '"alpha"'`, 1)

	if _, err := DB.Exec("UPDATE notes SET content = 'beta marker' WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	var rendered sql.NullString
	if err := DB.QueryRow("SELECT rendered_content FROM notes WHERE id = ?", id).Scan(&rendered); err != nil {
		t.Fatal(err)
	}
	if rendered.Valid {
		t.Fatal("content-only update did not invalidate rendered Markdown cache")
	}
	assertFTSCount(t, `notes_fts MATCH '"alpha"'`, 0)
	assertFTSCount(t, `notes_fts MATCH '"beta"'`, 1)

	if _, err := DB.Exec("DELETE FROM notes WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	assertFTSCount(t, `notes_fts MATCH '"beta"'`, 0)
}

func assertFTSCount(t *testing.T, condition string, want int) {
	t.Helper()
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM notes_fts WHERE " + condition).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("FTS count = %d, want %d", count, want)
	}
}

func TestNoteLockMigrationAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note-lock.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT NOT NULL, content TEXT NOT NULL, created_at DATETIME NOT NULL);
        INSERT INTO notes VALUES(1,'Existing note','Keep this content',CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	InitDB(path)
	var title, content string
	var locked bool
	if err := DB.QueryRow("SELECT title,content,locked FROM notes WHERE id=1").Scan(&title, &content, &locked); err != nil || locked || title != "Existing note" || content != "Keep this content" {
		t.Fatalf("migration changed legacy note: %s %s %v %v", title, content, locked, err)
	}
	if _, err := DB.Exec("UPDATE notes SET locked=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	DB.Close()
	InitDB(path)
	t.Cleanup(func() { DB.Close() })
	if err := DB.QueryRow("SELECT locked FROM notes WHERE id=1").Scan(&locked); err != nil || !locked {
		t.Fatalf("lock lost on restart: %v %v", locked, err)
	}
}
