package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestSeedAndCleanOnlyGeneratedNotes(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "seed.db")
	db.InitDB(databasePath)
	result, err := db.DB.Exec(
		"INSERT INTO users (username, password_hash, role, created_at) VALUES ('rick', X'00', 'admin', ?)",
		time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	if _, err := db.DB.Exec(
		"INSERT INTO notes (user_id, title, content, created_at) VALUES (?, 'Keep me', 'real note', ?)",
		userID, time.Now(),
	); err != nil {
		t.Fatal(err)
	}
	db.DB.Close()

	now := time.Date(2026, 9, 21, 15, 0, 0, 0, time.Local)
	stats, err := runSeed(seedConfig{
		database: databasePath, username: "RICK", count: 120,
		confirmTestData: true, now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 120 || stats.Pinned == 0 || stats.Trashed == 0 || stats.Batch == "" {
		t.Fatalf("unexpected seed stats: %+v", stats)
	}

	check, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	var notes, registered, untitled, oldNotes, searchMatches int
	queries := []struct {
		query string
		value *int
	}{
		{"SELECT COUNT(*) FROM notes", &notes},
		{"SELECT COUNT(*) FROM development_seed_notes", &registered},
		{"SELECT COUNT(*) FROM notes WHERE title = ''", &untitled},
		{"SELECT COUNT(*) FROM notes WHERE created_at < '2025-01-01'", &oldNotes},
		{`SELECT COUNT(*) FROM notes_fts WHERE notes_fts MATCH '"orchidsignal"'`, &searchMatches},
	}
	for _, query := range queries {
		if err := check.QueryRow(query.query).Scan(query.value); err != nil {
			t.Fatal(err)
		}
	}
	check.Close()
	if notes != 121 || registered != 120 || untitled == 0 || oldNotes == 0 || searchMatches == 0 {
		t.Fatalf("notes=%d registered=%d untitled=%d old=%d searchable=%d", notes, registered, untitled, oldNotes, searchMatches)
	}

	cleaned, err := runSeed(seedConfig{
		database: databasePath, username: "rick", count: 1,
		clean: true, confirmTestData: true, now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleaned.Deleted != 120 {
		t.Fatalf("deleted %d notes, want 120", cleaned.Deleted)
	}

	check, err = sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var title string
	if err := check.QueryRow("SELECT title FROM notes").Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Keep me" {
		t.Fatalf("remaining note = %q, want real note", title)
	}
}

func TestGeneratedNotesCoverStylesAndDates(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 0, 0, 0, time.UTC)
	var hasUntitled, hasCode, hasSearchMarker, hasOldDate bool
	for i := 0; i < 100; i++ {
		note := generateNote(i, now)
		hasUntitled = hasUntitled || note.Title == ""
		hasCode = hasCode || strings.Contains(note.Content, "```go")
		hasSearchMarker = hasSearchMarker || strings.Contains(strings.ToLower(note.Content), "orchidsignal")
		hasOldDate = hasOldDate || note.CreatedAt.Before(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	}
	if !hasUntitled || !hasCode || !hasSearchMarker || !hasOldDate {
		t.Fatalf("coverage untitled=%v code=%v marker=%v old-date=%v", hasUntitled, hasCode, hasSearchMarker, hasOldDate)
	}
}

func TestSeedSafetyChecks(t *testing.T) {
	if !productionDatabasePath("/var/lib/gnotes/gnotes.db") {
		t.Fatal("production database path was not detected")
	}
	if productionDatabasePath("./gnotes.db") {
		t.Fatal("local database path was classified as production")
	}
	if _, err := runSeed(seedConfig{database: "ignored.db", username: "rick", count: 1}); err == nil {
		t.Fatal("seed accepted missing confirmation")
	}
}
