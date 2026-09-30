package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
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

func TestArchiveAndColorKeepNoteRecoverable(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "archive-actions.db"))
	t.Cleanup(func() { db.DB.Close() })
	userID := insertPageTestUser(t, "archive-actions")
	result, err := db.DB.Exec(
		"INSERT INTO notes (user_id, title, content, created_at) VALUES (?, 'Keep me', 'Read later', ?)",
		userID, time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	noteID64, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	noteID := int(noteID64)
	auth := authSession{UserID: userID, Username: "archive-actions", CSRFToken: "csrf"}

	colorRequest := httptest.NewRequest(http.MethodPut, "/api/notes/color?id="+strconv.Itoa(noteID), strings.NewReader(`{"color":"sage"}`))
	colorRequest.Header.Set("Content-Type", "application/json")
	colorRequest.Header.Set("X-CSRF-Token", "csrf")
	colorRequest = colorRequest.WithContext(context.WithValue(colorRequest.Context(), authContextKey{}, auth))
	colorResponse := httptest.NewRecorder()
	updateNoteColorHandler(colorResponse, colorRequest)
	if colorResponse.Code != http.StatusNoContent {
		t.Fatalf("color status = %d: %s", colorResponse.Code, colorResponse.Body.String())
	}

	archiveRequest := httptest.NewRequest(http.MethodPost, "/api/notes/archive?id="+strconv.Itoa(noteID), nil)
	archiveRequest.Header.Set("X-CSRF-Token", "csrf")
	archiveRequest = archiveRequest.WithContext(context.WithValue(archiveRequest.Context(), authContextKey{}, auth))
	archiveResponse := httptest.NewRecorder()
	noteArchiveHandler(archiveResponse, archiveRequest)
	if archiveResponse.Code != http.StatusNoContent {
		t.Fatalf("archive status = %d: %s", archiveResponse.Code, archiveResponse.Body.String())
	}

	page := requestNotePage(t, pagedListNotesHandler, "/api/notes/page?limit=10", userID)
	if page.Total != 0 || len(page.Notes) != 0 {
		t.Fatalf("archived note remained active: %+v", page)
	}

	archivedRequest := httptest.NewRequest(http.MethodGet, "/api/notes/archived", nil)
	archivedRequest = archivedRequest.WithContext(context.WithValue(archivedRequest.Context(), authContextKey{}, auth))
	archivedResponse := httptest.NewRecorder()
	archivedNotesHandler(archivedResponse, archivedRequest)
	if archivedResponse.Code != http.StatusOK || !strings.Contains(archivedResponse.Body.String(), `"background_color":"sage"`) {
		t.Fatalf("archived response = %d: %s", archivedResponse.Code, archivedResponse.Body.String())
	}

	unarchiveRequest := httptest.NewRequest(http.MethodPost, "/api/notes/unarchive?id="+strconv.Itoa(noteID), nil)
	unarchiveRequest.Header.Set("X-CSRF-Token", "csrf")
	unarchiveRequest = unarchiveRequest.WithContext(context.WithValue(unarchiveRequest.Context(), authContextKey{}, auth))
	unarchiveResponse := httptest.NewRecorder()
	unarchiveNoteHandler(unarchiveResponse, unarchiveRequest)
	if unarchiveResponse.Code != http.StatusNoContent {
		t.Fatalf("unarchive status = %d: %s", unarchiveResponse.Code, unarchiveResponse.Body.String())
	}
	page = requestNotePage(t, pagedListNotesHandler, "/api/notes/page?limit=10", userID)
	if page.Total != 1 || page.Notes[0].BackgroundColor != "sage" {
		t.Fatalf("restored note = %+v", page)
	}
}
