package main

import (
	"bytes"
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

func transferRequest(method, target string, body []byte, userID int) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	return request.WithContext(context.WithValue(request.Context(), authContextKey{}, authSession{UserID: userID}))
}

func TestTransferRoundTripAndAccountIsolation(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "transfer.db"))
	t.Cleanup(func() { db.DB.Close() })
	source := insertPageTestUser(t, "transfer-source")
	destination := insertPageTestUser(t, "transfer-destination")
	other := insertPageTestUser(t, "transfer-other")
	when := time.Date(2026, 9, 1, 12, 34, 0, 0, time.UTC)
	for _, note := range []struct {
		title, content    string
		archived, deleted any
		pinned            bool
		color             string
	}{
		{"Active", "**Markdown**", nil, nil, true, "sage"},
		{"Archived", "Read later", when.Add(time.Hour), nil, false, "peach"},
		{"Recycled", "Recover later", nil, when.Add(time.Hour), false, ""},
	} {
		if _, err := db.DB.Exec(`INSERT INTO notes (user_id, title, content, created_at, archived_at, deleted_at, pinned, background_color)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, source, note.title, note.content, when, note.archived, note.deleted, note.pinned, note.color); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec("INSERT INTO notes (user_id, title, content, created_at) VALUES (?, 'Private', 'not yours', ?)", other, when); err != nil {
		t.Fatal(err)
	}

	export := httptest.NewRecorder()
	exportNotesHandler(export, transferRequest("GET", "/api/notes/export?format=gnotes", nil, source))
	if export.Code != 200 {
		t.Fatalf("export = %d: %s", export.Code, export.Body.String())
	}
	var file transferFile
	if err := json.Unmarshal(export.Body.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	if file.Version != transferVersion || len(file.Notes) != 3 {
		t.Fatalf("export = %+v", file)
	}
	for _, note := range file.Notes {
		if note.Title == "Private" {
			t.Fatal("other user's note exported")
		}
	}

	importFile := func(mode string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		importNotesHandler(response, transferRequest("POST", "/api/notes/import?format=gnotes&duplicates="+mode, body, destination))
		return response
	}
	first := importFile("skip", export.Body.Bytes())
	if first.Code != 200 || !strings.Contains(first.Body.String(), `"imported":3`) {
		t.Fatalf("first import = %d: %s", first.Code, first.Body.String())
	}
	second := importFile("skip", export.Body.Bytes())
	if second.Code != 200 || !strings.Contains(second.Body.String(), `"skipped":3`) {
		t.Fatalf("duplicate import = %d: %s", second.Code, second.Body.String())
	}
	var count, archived, deleted, pinned int
	if err := db.DB.QueryRow(`SELECT count(*), sum(archived_at IS NOT NULL), sum(deleted_at IS NOT NULL), sum(pinned)
		FROM notes WHERE user_id = ?`, destination).Scan(&count, &archived, &deleted, &pinned); err != nil {
		t.Fatal(err)
	}
	if count != 3 || archived != 1 || deleted != 1 || pinned != 1 {
		t.Fatalf("restored states = %d %d %d %d", count, archived, deleted, pinned)
	}
	copyResponse := importFile("copy", export.Body.Bytes())
	if copyResponse.Code != 200 || !strings.Contains(copyResponse.Body.String(), `"imported":3`) {
		t.Fatalf("copy import = %d: %s", copyResponse.Code, copyResponse.Body.String())
	}
	if err := db.DB.QueryRow("SELECT count(*) FROM notes WHERE user_id = ?", destination).Scan(&count); err != nil || count != 6 {
		t.Fatalf("copied count = %d: %v", count, err)
	}

	file.Notes[1].BackgroundColor = "invalid"
	invalid, _ := json.Marshal(file)
	failed := importFile("copy", invalid)
	if failed.Code != 400 {
		t.Fatalf("invalid import = %d: %s", failed.Code, failed.Body.String())
	}
	if err := db.DB.QueryRow("SELECT count(*) FROM notes WHERE user_id = ?", destination).Scan(&count); err != nil || count != 6 {
		t.Fatalf("invalid import modified notes: %d: %v", count, err)
	}
	selected := httptest.NewRecorder()
	exportNotesHandler(selected, transferRequest("GET", "/api/notes/export?format=gnotes&ids="+stringID(file.Notes[0].SourceID), nil, source))
	var one transferFile
	if selected.Code != 200 || json.Unmarshal(selected.Body.Bytes(), &one) != nil || len(one.Notes) != 1 {
		t.Fatalf("selected export = %d: %s", selected.Code, selected.Body.String())
	}
}

func TestReadableTransferFormats(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "readable-transfer.db"))
	t.Cleanup(func() { db.DB.Close() })
	source := insertPageTestUser(t, "readable-source")
	destination := insertPageTestUser(t, "readable-destination")
	when := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	for _, title := range []string{"First", "Second"} {
		if _, err := db.DB.Exec("INSERT INTO notes (user_id, title, content, created_at) VALUES (?, ?, ?, ?)", source, title, "Body for "+title, when); err != nil {
			t.Fatal(err)
		}
	}
	archive := httptest.NewRecorder()
	exportNotesHandler(archive, transferRequest("GET", "/api/notes/export?format=markdown", nil, source))
	if archive.Code != 200 || archive.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("archive export = %d: %s", archive.Code, archive.Body.String())
	}
	imported := httptest.NewRecorder()
	importNotesHandler(imported, transferRequest("POST", "/api/notes/import?format=zip&duplicates=skip", archive.Body.Bytes(), destination))
	if imported.Code != 200 || !strings.Contains(imported.Body.String(), `"imported":2`) {
		t.Fatalf("archive import = %d: %s", imported.Code, imported.Body.String())
	}

	var id int
	if err := db.DB.QueryRow("SELECT id FROM notes WHERE user_id = ? AND title = 'First'", source).Scan(&id); err != nil {
		t.Fatal(err)
	}
	plain := httptest.NewRecorder()
	exportNotesHandler(plain, transferRequest("GET", "/api/notes/export?format=text&ids="+strconv.Itoa(id), nil, source))
	if plain.Code != 200 || plain.Body.String() != "Body for First" {
		t.Fatalf("plain export = %d: %s", plain.Code, plain.Body.String())
	}
	textImport := httptest.NewRecorder()
	importNotesHandler(textImport, transferRequest("POST", "/api/notes/import?format=text&duplicates=skip&title=From+text", []byte("Plain body"), destination))
	if textImport.Code != 200 || !strings.Contains(textImport.Body.String(), `"imported":1`) {
		t.Fatalf("text import = %d: %s", textImport.Code, textImport.Body.String())
	}

	wrongUser := httptest.NewRecorder()
	exportNotesHandler(wrongUser, transferRequest("GET", "/api/notes/export?format=gnotes&ids="+strconv.Itoa(id), nil, destination))
	if wrongUser.Code != http.StatusNotFound {
		t.Fatalf("cross-account selected export = %d", wrongUser.Code)
	}
}

func stringID(value int) string { return strconv.Itoa(value) }
