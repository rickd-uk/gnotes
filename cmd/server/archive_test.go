package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestNoteArchiveUsesUserAndBrowserTimezone(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "archive.db"))
	t.Cleanup(func() { db.DB.Close() })
	userID := insertPageTestUser(t, "archivist")
	otherUserID := insertPageTestUser(t, "other-archivist")

	notes := []struct {
		userID    int
		createdAt time.Time
		deleted   bool
	}{
		{userID, time.Date(2026, 8, 31, 15, 30, 0, 0, time.UTC), false}, // September in Tokyo.
		{userID, time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC), false},
		{userID, time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC), false},
		{userID, time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC), true},
		{otherUserID, time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC), false},
	}
	for _, note := range notes {
		var deletedAt any
		if note.deleted {
			deletedAt = note.createdAt.Add(time.Hour)
		}
		if _, err := db.DB.Exec(
			"INSERT INTO notes (user_id, title, content, created_at, deleted_at) VALUES (?, 'archive', 'body', ?, ?)",
			note.userID, note.createdAt, deletedAt,
		); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/notes/archive?timezone=Asia%2FTokyo", nil)
	request = request.WithContext(context.WithValue(
		request.Context(), authContextKey{}, authSession{UserID: userID, Username: "archivist"},
	))
	response := httptest.NewRecorder()
	noteArchiveHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var archive archiveResponse
	if err := json.Unmarshal(response.Body.Bytes(), &archive); err != nil {
		t.Fatal(err)
	}
	if archive.Total != 3 {
		t.Fatalf("total = %d, want 3", archive.Total)
	}
	if len(archive.Months) != 2 || archive.Months[0].Key != "2026-10" || archive.Months[0].Count != 1 || archive.Months[1].Key != "2026-09" || archive.Months[1].Count != 2 {
		t.Fatalf("monthly archive = %+v", archive.Months)
	}
	if len(archive.Weeks) < 2 {
		t.Fatalf("weekly archive = %+v, want multiple weeks", archive.Weeks)
	}
}
