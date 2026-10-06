package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestActiveBulkRemoval(t *testing.T) {
	for _, scope := range []string{"date", "pinned", "all"} {
		t.Run(scope, func(t *testing.T) {
			_, _, login, _ := recoveryFixture(t)
			start := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
			type fixture struct {
				owner, pinned     int
				created           time.Time
				archived, deleted any
				want              bool
			}
			fixtures := []fixture{
				{1, 0, start.Add(-time.Second), nil, nil, scope == "all"},
				{1, 0, start, nil, nil, scope != "pinned"},
				{1, 0, start.Add(24*time.Hour - time.Second), nil, nil, scope != "pinned"},
				{1, 0, start.Add(24 * time.Hour), nil, nil, scope == "all"},
				{1, 1, start, nil, nil, scope != "date"},
				{1, 1, start.Add(-24 * time.Hour), nil, nil, scope != "date"},
				{1, 0, start, start, nil, false},
				{1, 1, start, start, nil, false},
				{1, 0, start, nil, start, false},
				{2, 1, start, nil, nil, false},
			}
			ids := []int{}
			for _, f := range fixtures {
				result, err := db.DB.Exec("INSERT INTO notes (user_id,title,content,tags,created_at,pinned,archived_at,deleted_at) VALUES (?,'Title','Body','keep',?,?,?,?)", f.owner, f.created, f.pinned, f.archived, f.deleted)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := result.LastInsertId()
				ids = append(ids, int(id))
			}
			path := "/api/notes/delete-active?scope=" + scope
			if scope == "date" {
				path += "&date=2026-10-06&timezone=Asia%2FTokyo"
			}
			response := authenticatedRequest(t, protect(deleteActiveNotesHandler, true), http.MethodPost, path, "", login, true)
			if response.Code != 200 {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			var result struct {
				Removed int   `json:"removed"`
				IDs     []int `json:"removed_ids"`
				Active  int   `json:"active_total"`
				Pinned  int   `json:"pinned_total"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			wantCount := 0
			for i, f := range fixtures {
				var deleted, archived bool
				if err := db.DB.QueryRow("SELECT deleted_at IS NOT NULL,archived_at IS NOT NULL FROM notes WHERE id=?", ids[i]).Scan(&deleted, &archived); err != nil {
					t.Fatal(err)
				}
				if deleted != (f.want || f.deleted != nil) || archived != (f.archived != nil) {
					t.Fatalf("note %d changed wrong state", ids[i])
				}
				if f.want {
					wantCount++
				}
			}
			wantPinned := 2
			if scope != "date" {
				wantPinned = 0
			}
			if result.Removed != wantCount || len(result.IDs) != wantCount || result.Active != 6-wantCount || result.Pinned != wantPinned {
				t.Fatalf("unexpected result: %+v", result)
			}
			for _, id := range result.IDs {
				restored := authenticatedRequest(t, protect(restoreNotesHandler, true), http.MethodPost, "/api/notes/restore?id="+strconv.Itoa(id), "", login, true)
				if restored.Code != 204 {
					t.Fatalf("restore: %d", restored.Code)
				}
				var content, tags string
				var pinned int
				var deleted bool
				db.DB.QueryRow("SELECT content,tags,pinned,deleted_at IS NOT NULL FROM notes WHERE id=?", id).Scan(&content, &tags, &pinned, &deleted)
				if content != "Body" || tags != "keep" || deleted || pinned != fixtures[id-ids[0]].pinned {
					t.Fatal("recovery lost note data")
				}
			}
		})
	}
}

func TestActiveBulkRemovalValidationAndDST(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	handler := protect(deleteActiveNotesHandler, true)
	for _, suffix := range []string{"", "?scope=wrong", "?scope=all&date=2026-10-06", "?scope=pinned&timezone=UTC", "?scope=date&date=2026-02-30&timezone=UTC", "?scope=date&date=2026-03-08", "?scope=date&date=2026-03-08&timezone=Invalid"} {
		response := authenticatedRequest(t, handler, http.MethodPost, "/api/notes/delete-active"+suffix, "", login, true)
		if response.Code != 400 {
			t.Fatalf("%s: %d", suffix, response.Code)
		}
	}
	if response := authenticatedRequest(t, handler, http.MethodPost, "/api/notes/delete-active?scope=all", "", login, false); response.Code != 403 {
		t.Fatalf("CSRF: %d", response.Code)
	}
	if response := authenticatedRequest(t, handler, http.MethodGet, "/api/notes/delete-active?scope=all", "", login, true); response.Code != 405 {
		t.Fatalf("method: %d", response.Code)
	}
	unauth := httptest.NewRecorder()
	handler(unauth, httptest.NewRequest(http.MethodPost, "/api/notes/delete-active?scope=all", nil))
	if unauth.Code != 401 {
		t.Fatalf("auth: %d", unauth.Code)
	}
	start := time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)
	end := start.Add(23 * time.Hour)
	for _, created := range []time.Time{start.Add(-time.Second), start, end.Add(-time.Second), end} {
		mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'DST','body',?)", created)
	}
	response := authenticatedRequest(t, handler, http.MethodPost, "/api/notes/delete-active?scope=date&date=2026-03-08&timezone=America%2FNew_York", "", login, true)
	var result struct {
		Removed int `json:"removed"`
	}
	json.Unmarshal(response.Body.Bytes(), &result)
	if response.Code != 200 || result.Removed != 2 {
		t.Fatalf("DST %d %s", response.Code, response.Body.String())
	}
}
