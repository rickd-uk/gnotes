package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"gnotes/internal/db"
	"gnotes/internal/models"
)

func tagsTestRequest(method, target string, body []byte, userID int) *http.Request {
	r := transferRequest(method, target, body, userID)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestTagsOwnershipValidationAndAutosave(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	created := httptest.NewRecorder()
	createNoteHandler(created, tagsTestRequest("POST", "/api/notes/create", []byte(`{"title":"Tagged note","content":"alpha","tags":["#Work","Ideas","work"]}`), 1))
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var note models.Note
	json.Unmarshal(created.Body.Bytes(), &note)
	if !reflect.DeepEqual(note.Tags, models.Tags{"ideas", "work"}) {
		t.Fatalf("tags: %v", note.Tags)
	}
	endpoint := "/api/notes/tags?id=" + strconv.Itoa(note.ID)
	if got := authenticatedRequest(t, protect(updateNoteTagsHandler, true), "PUT", endpoint, `{"tags":["reading"]}`, login, false); got.Code != 403 {
		t.Fatal("tags require CSRF")
	}
	other := httptest.NewRecorder()
	updateNoteTagsHandler(other, tagsTestRequest("PUT", endpoint, []byte(`{"tags":["stolen"]}`), 2))
	if other.Code != 404 {
		t.Fatalf("other account changed note: %d", other.Code)
	}
	for _, input := range []string{`{"tags":null}`, `{"tags":["<script>"]}`, `{"tags":[""]}`, `{"tags":["1","2","3","4","5","6","7","8","9","10","11"]}`} {
		got := authenticatedRequest(t, protect(updateNoteTagsHandler, true), "PUT", endpoint, input, login, true)
		if got.Code != 400 {
			t.Fatalf("invalid tags accepted: %s", input)
		}
	}
	got := authenticatedRequest(t, protect(updateNoteTagsHandler, true), "PUT", endpoint, `{"tags":["Reading List","日本語"]}`, login, true)
	if got.Code != 200 {
		t.Fatalf("save tags: %d %s", got.Code, got.Body.String())
	}
	updated := httptest.NewRecorder()
	updateNoteHandler(updated, tagsTestRequest("PUT", "/api/notes/update?id="+strconv.Itoa(note.ID), []byte(`{"title":"Autosaved","content":"alpha beta"}`), 1))
	if updated.Code != 204 {
		t.Fatalf("autosave: %d", updated.Code)
	}
	var tags models.Tags
	var title, content string
	db.DB.QueryRow("SELECT tags,title,content FROM notes WHERE id=?", note.ID).Scan(&tags, &title, &content)
	if !reflect.DeepEqual(tags, models.Tags{"reading-list", "日本語"}) || title != "Autosaved" || content != "alpha beta" {
		t.Fatal("autosave lost tags or changed content unexpectedly")
	}
	got = authenticatedRequest(t, protect(updateNoteTagsHandler, true), "PUT", endpoint, `{"tags":[]}`, login, true)
	if got.Code != 200 {
		t.Fatal("cannot clear tags")
	}
}

func TestTagFilteringCountsPaginationAndTransfer(t *testing.T) {
	recoveryFixture(t)
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		var archived, deleted any
		if i == 3 {
			archived = base
		}
		if i == 4 {
			deleted = base
		}
		mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,archived_at,deleted_at,tags) VALUES(1,'alpha','body',?,?,?,?)", base.Add(time.Duration(i)*time.Minute), archived, deleted, models.Tags{"work"})
	}
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,tags) VALUES(2,'alpha','private',?,?)", base, models.Tags{"private", "work"})
	list := httptest.NewRecorder()
	tagsHandler(list, transferRequest("GET", "/api/tags", nil, 1))
	if list.Code != 200 || list.Body.String() != "[{\"name\":\"work\",\"count\":3}]\n" {
		t.Fatalf("tag list leaked accounts or states: %d %s", list.Code, list.Body.String())
	}
	seen := map[int]bool{}
	cursor := ""
	for {
		page := requestNotePage(t, pagedSearchNotesHandler, "/api/notes/search-page?tag=%23WORK&q=alpha&limit=1&cursor="+url.QueryEscape(cursor), 1)
		if page.Total != 3 || page.MatchCount != 3 || len(page.Notes) != 1 || seen[page.Notes[0].ID] || !reflect.DeepEqual(page.Notes[0].Tags, models.Tags{"work"}) {
			t.Fatalf("bad tagged page: %+v", page)
		}
		seen[page.Notes[0].ID] = true
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 3 {
		t.Fatal("tag pagination dropped notes")
	}
	archived := requestNotePage(t, pagedSearchNotesHandler, "/api/notes/search-page?archive=1&tag=work", 1)
	if archived.Total != 1 || !reflect.DeepEqual(archived.Notes[0].Tags, models.Tags{"work"}) {
		t.Fatal("archive lost tags")
	}
	trash := requestNotePage(t, pagedTrashNotesHandler, "/api/notes/trash-page", 1)
	if trash.Total != 1 || !reflect.DeepEqual(trash.Notes[0].Tags, models.Tags{"work"}) {
		t.Fatal("recycle bin lost tags")
	}
	for _, format := range []string{"gnotes", "markdown", "text"} {
		export := httptest.NewRecorder()
		exportNotesHandler(export, transferRequest("GET", "/api/notes/export?format="+format+"&ids=1", nil, 1))
		if export.Code != 200 {
			t.Fatalf("export: %d", export.Code)
		}
		importFormat := format
		if format != "gnotes" {
			if export.Header().Get("Content-Type") != "application/zip" {
				t.Fatal("tagged readable export needs a metadata manifest")
			}
			importFormat = "zip"
		}
		imported := httptest.NewRecorder()
		importNotesHandler(imported, transferRequest("POST", "/api/notes/import?format="+importFormat+"&duplicates=copy", export.Body.Bytes(), 2))
		if imported.Code != 200 {
			t.Fatalf("import: %d %s", imported.Code, imported.Body.String())
		}
		var tags models.Tags
		if err := db.DB.QueryRow("SELECT tags FROM notes WHERE user_id=2 ORDER BY id DESC LIMIT 1").Scan(&tags); err != nil || !reflect.DeepEqual(tags, models.Tags{"work"}) {
			t.Fatalf("round trip lost tags: %v %v", tags, err)
		}
	}
	var legacy transferFile
	if err := decodeTransferFile([]byte(`{"format":"gnotes","version":1,"notes":[]}`), &legacy); err != nil {
		t.Fatal("legacy exports must remain importable", err)
	}
}
