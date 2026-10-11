package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gnotes/internal/db"
	"gnotes/internal/models"
)

func TestNoteLockProtectionAndPersistence(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,tags,created_at) VALUES(1,'Protected','- [ ] original','[\"work\"]',?)", time.Now().UTC())
	endpoint := "/api/notes/lock?id=1"
	handler := protect(lockNoteHandler, true)
	if got := authenticatedRequest(t, handler, "PUT", endpoint, `{"locked":true}`, login, false); got.Code != 403 {
		t.Fatal("lock must require CSRF")
	}
	for _, input := range []string{`{}`, `{"locked":null}`, `{"locked":"yes"}`} {
		if got := authenticatedRequest(t, handler, "PUT", endpoint, input, login, true); got.Code != 400 {
			t.Fatalf("invalid lock: %d", got.Code)
		}
	}
	for i := 0; i < 2; i++ {
		if got := authenticatedRequest(t, handler, "PUT", endpoint, `{"locked":true}`, login, true); got.Code != 200 {
			t.Fatalf("lock: %d %s", got.Code, got.Body.String())
		}
	}
	other := httptest.NewRecorder()
	lockNoteHandler(other, tagsTestRequest("PUT", endpoint, []byte(`{"locked":false}`), 2))
	if other.Code != 404 {
		t.Fatalf("another user unlocked: %d", other.Code)
	}
	for _, test := range []struct {
		handler        http.HandlerFunc
		endpoint, body string
	}{
		{updateNoteHandler, "/api/notes/update?id=1", `{"title":"Changed","content":"- [x] original","locked":false}`},
		{updateNoteTagsHandler, "/api/notes/tags?id=1", `{"tags":["changed"]}`},
		{updateNoteColorHandler, "/api/notes/color?id=1", `{"color":"sage"}`},
	} {
		got := authenticatedRequest(t, protect(test.handler, true), "PUT", test.endpoint, test.body, login, true)
		if got.Code != http.StatusLocked {
			t.Fatalf("locked mutation %s: %d %s", test.endpoint, got.Code, got.Body.String())
		}
		other := httptest.NewRecorder()
		test.handler(other, tagsTestRequest("PUT", test.endpoint, []byte(test.body), 2))
		if other.Code != 404 {
			t.Fatalf("lock leaked across accounts: %d", other.Code)
		}
	}
	var title, content, color string
	var locked bool
	var tags models.Tags
	if err := db.DB.QueryRow("SELECT title,content,background_color,tags,locked FROM notes WHERE id=1").Scan(&title, &content, &color, &tags, &locked); err != nil || title != "Protected" || content != "- [ ] original" || color != "" || !locked || len(tags) != 1 || tags[0] != "work" {
		t.Fatalf("locked data changed: %s %s %s %v %v %v", title, content, color, tags, locked, err)
	}
	for _, test := range []struct {
		handler  http.HandlerFunc
		endpoint string
	}{
		{listNotesHandler, "/api/notes"}, {pagedListNotesHandler, "/api/notes/page"}, {getNoteHandler, "/api/notes/get?id=1"}, {pagedSearchNotesHandler, "/api/notes/search-page?q=original"}, {searchNotesHandler, "/api/notes/search?q=original"},
	} {
		got := authenticatedRequest(t, protect(test.handler, false), "GET", test.endpoint, "", login, false)
		if got.Code != 200 || !strings.Contains(got.Body.String(), `"locked":true`) {
			t.Fatalf("missing lock in %s: %d %s", test.endpoint, got.Code, got.Body.String())
		}
	}
	// Exports and imported copies retain protection; legacy v3 files remain valid.
	notes, err := loadTransferNotesFiltered(1, nil, true)
	if err != nil || len(notes) != 1 || !notes[0].Locked {
		t.Fatalf("export lost lock: %v %v", notes, err)
	}
	body, _ := json.Marshal(transferFile{Format: "gnotes", Version: transferVersion, Notes: notes})
	imported := httptest.NewRecorder()
	importNotesHandler(imported, transferRequest("POST", "/api/notes/import?format=gnotes&duplicates=skip", body, 2))
	if imported.Code != 200 {
		t.Fatalf("import: %d %s", imported.Code, imported.Body.String())
	}
	var copied bool
	if err := db.DB.QueryRow("SELECT locked FROM notes WHERE user_id=2 AND title='Protected'").Scan(&copied); err != nil || !copied {
		t.Fatalf("imported lock: %v %v", copied, err)
	}
	notes[0].Locked = false
	body, _ = json.Marshal(transferFile{Format: "gnotes", Version: 3, Notes: notes})
	imported = httptest.NewRecorder()
	importNotesHandler(imported, transferRequest("POST", "/api/notes/import?format=gnotes&duplicates=skip", body, 2))
	if imported.Code != 200 {
		t.Fatalf("legacy import: %d", imported.Code)
	}
	mustRecoveryExec(t, "UPDATE notes SET tags='[]' WHERE id=1")
	readable := authenticatedRequest(t, protect(exportNotesHandler, false), "GET", "/api/notes/export?format=markdown&ids=1", "", login, false)
	if readable.Code != 200 || readable.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("locked readable export lost metadata: %d %s", readable.Code, readable.Header().Get("Content-Type"))
	}
	archive, err := zip.NewReader(bytes.NewReader(readable.Body.Bytes()), int64(readable.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var manifest transferFile
	for _, entry := range archive.File {
		if entry.Name == "gnotes-manifest.json" {
			stream, _ := entry.Open()
			err = json.NewDecoder(stream).Decode(&manifest)
			stream.Close()
		}
	}
	if err != nil || len(manifest.Notes) != 1 || !manifest.Notes[0].Locked {
		t.Fatal("readable manifest lost lock")
	}

	mustRecoveryExec(t, "UPDATE notes SET archived_at=? WHERE id=1", time.Now().UTC())
	got := authenticatedRequest(t, protect(archivedNotesHandler, false), "GET", "/api/notes/archive", "", login, false)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"locked":true`) {
		t.Fatal("archive lost lock")
	}
	if got := authenticatedRequest(t, handler, "PUT", endpoint, `{"locked":false}`, login, true); got.Code != 200 {
		t.Fatal("archive cannot unlock")
	}
	mustRecoveryExec(t, "UPDATE notes SET archived_at=NULL WHERE id=1")
	got = authenticatedRequest(t, protect(updateNoteHandler, true), "PUT", "/api/notes/update?id=1", `{"title":"Unlocked","content":"Editable again"}`, login, true)
	if got.Code != 204 {
		t.Fatalf("unlock did not restore editing: %d", got.Code)
	}
}
