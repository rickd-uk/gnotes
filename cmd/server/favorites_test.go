package main

import (
	"encoding/json"
	"gnotes/internal/db"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestFavoritesIndependentArchiveIsolationAndTransfer(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "favorites.db"))
	t.Cleanup(func() { db.DB.Close() })
	owner := insertPageTestUser(t, "favorite-owner")
	other := insertPageTestUser(t, "favorite-other")
	now := time.Now().UTC()
	for _, row := range []struct {
		title             string
		user              int
		favorite          bool
		archived, deleted any
	}{
		{"Active", owner, true, nil, nil}, {"Archived", owner, true, now, nil},
		{"Ordinary", owner, false, nil, nil}, {"Deleted", owner, true, nil, now}, {"Private", other, true, nil, nil},
	} {
		_, err := db.DB.Exec("INSERT INTO notes(user_id,title,content,created_at,favorited,archived_at,deleted_at,tags) VALUES(?,?,?,?,?,?,?,?)", row.user, row.title, "important text", now, row.favorite, row.archived, row.deleted, `["work"]`)
		if err != nil {
			t.Fatal(err)
		}
	}
	page := requestNotePage(t, pagedListNotesHandler, "/api/notes/page?favorites=1&limit=1", owner)
	if page.Total != 2 || page.View != "favorites" || len(page.Notes) != 1 || !page.Notes[0].Favorited || page.NextCursor == "" {
		t.Fatalf("favorites page: %+v", page)
	}
	for _, path := range []string{"/api/notes/search-page?favorites=1&q=important", "/api/notes/search-page?favorites=1&tag=work"} {
		page = requestNotePage(t, pagedSearchNotesHandler, path, owner)
		if page.Total != 2 || len(page.Notes) != 2 {
			t.Fatalf("filtered favorites %s: %+v", path, page)
		}
	}
	page = requestNotePage(t, pagedListNotesHandler, "/api/notes/page", owner)
	if page.Total != 2 {
		t.Fatalf("active notes changed: %+v", page)
	}
	var activeID, privateID, deletedID int
	db.DB.QueryRow("SELECT id FROM notes WHERE title='Active'").Scan(&activeID)
	db.DB.QueryRow("SELECT id FROM notes WHERE title='Private'").Scan(&privateID)
	db.DB.QueryRow("SELECT id FROM notes WHERE title='Deleted'").Scan(&deletedID)
	for _, tc := range []struct {
		id   int
		body string
		want int
	}{
		{activeID, `{"favorited":true}`, 204}, {activeID, `{"favorited":true}`, 204},
		{privateID, `{"favorited":false}`, 404}, {deletedID, `{"favorited":false}`, 404},
		{activeID, `{}`, 400},
	} {
		response := httptest.NewRecorder()
		request := transferRequest("PUT", "/api/notes/favorite?id="+stringID(tc.id), []byte(tc.body), owner)
		request.Header.Set("Content-Type", "application/json")
		favoriteNoteHandler(response, request)
		if response.Code != tc.want {
			t.Fatalf("favorite %d = %d: %s", tc.id, response.Code, response.Body.String())
		}
	}
	export := httptest.NewRecorder()
	exportNotesHandler(export, transferRequest("GET", "/api/notes/export?format=gnotes", nil, owner))
	var file transferFile
	if export.Code != 200 || json.Unmarshal(export.Body.Bytes(), &file) != nil {
		t.Fatal(export.Body.String())
	}
	imported := httptest.NewRecorder()
	importNotesHandler(imported, transferRequest("POST", "/api/notes/import?format=gnotes&duplicates=copy", export.Body.Bytes(), other))
	if imported.Code != 200 {
		t.Fatal(imported.Body.String())
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM notes WHERE user_id=? AND favorited=1 AND archived_at IS NOT NULL AND deleted_at IS NULL", other).Scan(&count); err != nil || count != 1 {
		t.Fatalf("archived favorite transfer: %d %v", count, err)
	}
	for _, version := range []int{1, 2} {
		file.Version = version
		for i := range file.Notes {
			file.Notes[i].Favorited = false
		}
		body, _ := json.Marshal(file)
		response := httptest.NewRecorder()
		importNotesHandler(response, transferRequest("POST", "/api/notes/import?format=gnotes&duplicates=copy", body, other))
		if response.Code != 200 {
			t.Fatalf("legacy v%d: %s", version, response.Body.String())
		}
	}
}
