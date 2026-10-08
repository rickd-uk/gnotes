package main

import (
	"encoding/json"
	"gnotes/internal/db"
	"net/http"
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
	for _, tc := range []struct {
		path   string
		search bool
		want   int
	}{
		{"/api/notes/page?hide_favorites=1&limit=1", false, 1},
		{"/api/notes/page?archive=1&hide_favorites=1", false, 0},
		{"/api/notes/page?archive=1", false, 1},
		{"/api/notes/page?favorites=1&hide_favorites=1", false, 2},
		{"/api/notes/search-page?hide_favorites=1&q=important", true, 1},
		{"/api/notes/search-page?archive=1&hide_favorites=1&tag=work", true, 0},
		{"/api/notes/search-page?favorites=1&hide_favorites=1&q=important", true, 2},
	} {
		handler := pagedListNotesHandler
		if tc.search {
			handler = pagedSearchNotesHandler
		}
		page := requestNotePage(t, handler, tc.path, owner)
		if page.Total != tc.want || len(page.Notes) != tc.want {
			t.Fatalf("favorite visibility %s: %+v", tc.path, page)
		}
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

func TestFavoriteDateNotes(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "archive"}[archived], func(t *testing.T) {
			_, _, login, _ := recoveryFixture(t)
			start := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
			insert := func(owner, pinned int, created time.Time, archive, deleted any) int {
				result, err := db.DB.Exec("INSERT INTO notes(user_id,title,content,created_at,pinned,archived_at,deleted_at) VALUES(?,'Date note','Body',?,?,?,?)", owner, created, pinned, archive, deleted)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := result.LastInsertId()
				return int(id)
			}
			var archive any
			if archived {
				archive = start
			}
			ids := []int{}
			for i := 0; i < 65; i++ {
				ids = append(ids, insert(1, 0, start.Add(time.Duration(i)*time.Second), archive, nil))
			}
			excluded := []int{insert(2, 0, start, archive, nil), insert(1, 0, start, archive, start)}
			if archived {
				excluded = append(excluded, insert(1, 0, start, nil, nil), insert(1, 0, start, start.Add(-time.Second), nil), insert(1, 0, start, start.Add(24*time.Hour), nil))
				ids = append(ids, insert(1, 1, start, start, nil))
			} else {
				excluded = append(excluded, insert(1, 1, start, nil, nil), insert(1, 0, start, start, nil), insert(1, 0, start.Add(-time.Second), nil, nil), insert(1, 0, start.Add(24*time.Hour), nil, nil))
			}
			path := "/api/notes/favorite-date?date=2026-10-06&timezone=Asia%2FTokyo"
			if archived {
				path += "&archive=1"
			}
			for attempt := 0; attempt < 2; attempt++ {
				response := authenticatedRequest(t, protect(favoriteDateNotesHandler, true), http.MethodPost, path, "", login, true)
				var result struct {
					Added int `json:"added"`
				}
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
					t.Fatalf("%d %s", response.Code, response.Body.String())
				}
				want := len(ids)
				if attempt == 1 {
					want = 0
				}
				if result.Added != want {
					t.Fatalf("added %d want %d", result.Added, want)
				}
			}
			for _, id := range ids {
				var favorite bool
				db.DB.QueryRow("SELECT favorited FROM notes WHERE id=?", id).Scan(&favorite)
				if !favorite {
					t.Fatalf("note %d omitted", id)
				}
			}
			for _, id := range excluded {
				var favorite bool
				db.DB.QueryRow("SELECT favorited FROM notes WHERE id=?", id).Scan(&favorite)
				if favorite {
					t.Fatalf("note %d should be excluded", id)
				}
			}
			for _, bad := range []string{"?date=bad&timezone=Asia%2FTokyo", "?date=2026-10-06", "?date=2026-10-06&timezone=bad", "?date=2026-10-06&timezone=UTC&archive=bad"} {
				response := authenticatedRequest(t, protect(favoriteDateNotesHandler, true), http.MethodPost, "/api/notes/favorite-date"+bad, "", login, true)
				if response.Code != 400 {
					t.Fatalf("invalid date request: %d", response.Code)
				}
			}
			response := authenticatedRequest(t, protect(favoriteDateNotesHandler, true), http.MethodPost, path, "", login, false)
			if response.Code != 403 {
				t.Fatalf("missing CSRF accepted: %d", response.Code)
			}
		})
	}
}
