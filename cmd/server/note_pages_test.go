package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestPagedNotesTraversesLargeCollectionWithoutDuplicates(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "pages.db"))
	t.Cleanup(func() { db.DB.Close() })
	userID := insertPageTestUser(t, "reader")
	otherUserID := insertPageTestUser(t, "other")

	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 125; i++ {
		if _, err := tx.Exec(
			"INSERT INTO notes (user_id, title, content, created_at, pinned) VALUES (?, ?, ?, ?, ?)",
			userID, fmt.Sprintf("Note %03d", i), fmt.Sprintf("body %03d", i), base.Add(-time.Duration(i)*time.Minute), i%31 == 0,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(
		"INSERT INTO notes (user_id, title, content, created_at) VALUES (?, 'private', 'other user', ?)",
		otherUserID, base.Add(time.Hour),
	); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	seen := make(map[int]bool)
	cursor := ""
	for pageNumber := 0; ; pageNumber++ {
		path := "/api/notes/page?limit=17"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		response := requestNotePage(t, pagedListNotesHandler, path, userID)
		if response.Total != 125 {
			t.Fatalf("page %d total = %d, want 125", pageNumber, response.Total)
		}
		if len(response.Notes) > 17 {
			t.Fatalf("page %d returned %d notes, want at most 17", pageNumber, len(response.Notes))
		}
		for _, note := range response.Notes {
			if seen[note.ID] {
				t.Fatalf("note %d appeared twice", note.ID)
			}
			seen[note.ID] = true
			if note.HTMLContent == "" {
				t.Fatalf("note %d has no rendered content", note.ID)
			}
		}
		cursor = response.NextCursor
		if cursor == "" {
			break
		}
		if pageNumber > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 125 {
		t.Fatalf("visited %d unique notes, want 125", len(seen))
	}

	var cached int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id = ? AND rendered_content IS NOT NULL", userID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if cached != 125 {
		t.Fatalf("cached rendered content for %d notes, want 125", cached)
	}
}

func TestPagedSearchPreservesSubstringAndCaseSemantics(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "search-pages.db"))
	t.Cleanup(func() { db.DB.Close() })
	userID := insertPageTestUser(t, "searcher")
	now := time.Now()
	for _, note := range []struct{ title, content string }{
		{"Alpha project", "ordinary"},
		{"lower", "contains alpha here"},
		{"case", "contains ALPHA here"},
		{"unrelated", "nothing"},
	} {
		if _, err := db.DB.Exec(
			"INSERT INTO notes (user_id, title, content, created_at) VALUES (?, ?, ?, ?)",
			userID, note.title, note.content, now,
		); err != nil {
			t.Fatal(err)
		}
	}

	page := requestNotePage(t, pagedSearchNotesHandler, "/api/notes/search-page?q=alpha&scope=all", userID)
	if page.Total != 3 || len(page.Notes) != 3 {
		t.Fatalf("case-insensitive search returned total=%d notes=%d, want 3", page.Total, len(page.Notes))
	}
	page = requestNotePage(t, pagedSearchNotesHandler, "/api/notes/search-page?q=alpha&scope=all&match_case=true", userID)
	if page.Total != 1 || len(page.Notes) != 1 || page.Notes[0].Title != "lower" {
		t.Fatalf("case-sensitive search result = %+v, want one lowercase content match", page)
	}
}

func TestCursorValidation(t *testing.T) {
	cursor, err := encodeCursor(activeNoteCursor{Pinned: true, CreatedAt: time.Now(), ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	decoded, present, err := decodeActiveNoteCursor(cursor)
	if err != nil || !present || decoded.ID != 7 || !decoded.Pinned {
		t.Fatalf("decoded cursor = %+v, present=%v, error=%v", decoded, present, err)
	}
	if _, _, err := decodeActiveNoteCursor(cursor + "e30"); err == nil {
		t.Fatal("accepted a cursor containing trailing data")
	}
	if ftsSearchEligible("ab") {
		t.Fatal("two-character search should not use trigram FTS")
	}
	if !ftsSearchEligible("alpha 123") {
		t.Fatal("ordinary search should use trigram FTS")
	}
	if !ftsSearchEligible("alpha-beta") {
		t.Fatal("punctuated substring search should use trigram FTS")
	}
}

func insertPageTestUser(t *testing.T, username string) int {
	t.Helper()
	result, err := db.DB.Exec(
		"INSERT INTO users (username, password_hash, role, created_at) VALUES (?, X'00', 'user', ?)",
		username, time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func requestNotePage(t *testing.T, handler http.HandlerFunc, path string, userID int) notePage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request = request.WithContext(context.WithValue(
		request.Context(), authContextKey{}, authSession{UserID: userID, Username: "test"},
	))
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s returned %d: %s", path, response.Code, response.Body.String())
	}
	var page notePage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func BenchmarkPagedListWithFiveThousandNotes(b *testing.B) {
	db.InitDB(filepath.Join(b.TempDir(), "benchmark.db"))
	b.Cleanup(func() { db.DB.Close() })
	userID := insertPageBenchmarkUser(b)
	tx, err := db.DB.Begin()
	if err != nil {
		b.Fatal(err)
	}
	base := time.Now()
	for i := 0; i < 5000; i++ {
		if _, err := tx.Exec(
			"INSERT INTO notes (user_id, title, content, rendered_content, created_at) VALUES (?, ?, ?, ?, ?)",
			userID, fmt.Sprintf("Note %04d", i), "benchmark body", "<p>benchmark body</p>", base.Add(-time.Duration(i)*time.Second),
		); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		request := httptest.NewRequest(http.MethodGet, "/api/notes/page?limit=50", nil)
		request = request.WithContext(context.WithValue(
			request.Context(), authContextKey{}, authSession{UserID: userID, Username: "bench"},
		))
		response := httptest.NewRecorder()
		pagedListNotesHandler(response, request)
		if response.Code != http.StatusOK {
			b.Fatalf("status = %d: %s", response.Code, response.Body.String())
		}
	}
}

func insertPageBenchmarkUser(b *testing.B) int {
	b.Helper()
	result, err := db.DB.Exec(
		"INSERT INTO users (username, password_hash, role, created_at) VALUES ('bench', X'00', 'user', ?)",
		time.Now(),
	)
	if err != nil {
		b.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		b.Fatal(err)
	}
	return int(id)
}
