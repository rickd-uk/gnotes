package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gnotes/internal/db"
	"gnotes/internal/models"
)

func TestNoteRevisionPreventsLostEdits(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Original','Original text',?)", time.Now().UTC())
	save := func(body string, who testLogin) *http.Response {
		return authenticatedRequest(t, protect(updateNoteHandler, true), "PUT", "/api/notes/update?id=1", body, who, true).Result()
	}
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"title":"New","content":"Text"}`, 428},
		{`{"title":"New","content":"Text","revision":-1}`, 400},
		{`{"title":"New","content":"Text","revision":0}`, 204},
		{`{"title":"Stale","content":"Old tab","revision":0}`, 409},
		// An identical retry after a lost response must not become a conflict.
		{`{"title":"New","content":"Text","revision":0}`, 204},
	} {
		response := save(test.body, login)
		response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatalf("%s: got %d, want %d", test.body, response.StatusCode, test.status)
		}
		if test.status == 204 && response.Header.Get("X-Gnotes-Revision") != "1" {
			t.Fatalf("retry changed revision: %v", response.Header)
		}
	}
	other := httptest.NewRecorder()
	updateNoteHandler(other, tagsTestRequest("PUT", "/api/notes/update?id=1", []byte(`{"title":"Other","content":"No","revision":1}`), 2))
	if other.Code != 404 {
		t.Fatalf("cross-account update: %d", other.Code)
	}
	var content string
	var revision int64
	if err := db.DB.QueryRow("SELECT content,revision FROM notes WHERE id=1").Scan(&content, &revision); err != nil || content != "Text" || revision != 1 {
		t.Fatalf("stale write changed note: %q %d %v", content, revision, err)
	}
	for _, endpoint := range []string{"/api/notes/get?id=1", "/api/notes/page", "/api/notes/search-page?q=Text"} {
		handler := getNoteHandler
		if strings.Contains(endpoint, "/page") {
			handler = pagedListNotesHandler
		}
		if strings.Contains(endpoint, "search-page") {
			handler = pagedSearchNotesHandler
		}
		response := authenticatedRequest(t, protect(handler, false), "GET", endpoint, "", login, false)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"revision":1`) {
			t.Fatalf("missing revision at %s: %d %s", endpoint, response.Code, response.Body.String())
		}
	}
	for _, body := range []string{
		`{"title":"New","content":"Text","tags":["Work"],"revision":1}`,
		`{"title":"New","content":"Text","tags":["Work"],"revision":1}`,
	} {
		response := save(body, login)
		response.Body.Close()
		if response.StatusCode != 204 || response.Header.Get("X-Gnotes-Revision") != "2" {
			t.Fatalf("normalized-tag retry: %d %v", response.StatusCode, response.Header)
		}
	}
}

func TestConcurrentNoteSavesHaveOneWinner(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at) VALUES(1,'Original','Original',?)", time.Now().UTC())
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, body := range []string{"First window", "Second window"} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			response := authenticatedRequest(t, protect(updateNoteHandler, true), "PUT", "/api/notes/update?id=1", fmt.Sprintf(`{"title":"Edit","content":%q,"revision":0}`, body), login, true)
			statuses <- response.Code
		}(body)
	}
	wg.Wait()
	a, b := <-statuses, <-statuses
	if !((a == 204 && b == 409) || (a == 409 && b == 204)) {
		t.Fatalf("concurrent outcomes %d, %d", a, b)
	}
}

func TestDraftRevisionRejectsEqualVersionsAndResurrection(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	save := func(title string, version, revision int64) int {
		return authenticatedRequest(t, protect(updateDraftHandler, true), "PUT", "/api/draft/update", fmt.Sprintf(`{"title":%q,"content":"body","version":%d,"revision":%d}`, title, version, revision), login, true).Code
	}
	if got := save("First window", 100, 0); got != 204 {
		t.Fatalf("first save: %d", got)
	}
	if got := save("Second window", 100, 0); got != 409 {
		t.Fatalf("equal-version overwrite: %d", got)
	}
	if got := save("First window", 100, 0); got != 204 {
		t.Fatalf("idempotent retry: %d", got)
	}
	// Client clock values do not decide who owns the next server revision.
	if got := save("Next edit", 1, 1); got != 204 {
		t.Fatalf("clock-independent save: %d", got)
	}
	finish := authenticatedRequest(t, protect(finalizeDraftHandler, true), "POST", "/api/draft/finalize", `{"version":1,"revision":1}`, login, true)
	if finish.Code != 409 {
		t.Fatalf("stale finalization: %d", finish.Code)
	}
	finish = authenticatedRequest(t, protect(finalizeDraftHandler, true), "POST", "/api/draft/finalize", `{"version":1,"revision":2}`, login, true)
	if finish.Code != 201 || finish.Header().Get("X-Gnotes-Revision") != "3" {
		t.Fatalf("finalization: %d %s", finish.Code, finish.Body.String())
	}
	if got := save("Delayed old request", 999999, 0); got != 409 {
		t.Fatalf("finished draft resurrected: %d", got)
	}
	var draft noteDraft
	if err := json.Unmarshal([]byte(getTestDraft(t, login)), &draft); err != nil || draft.Version != 0 || draft.Revision != 3 || draft.Title != "" || draft.Content != "" {
		t.Fatalf("draft tombstone: %+v %v", draft, err)
	}
	if got := save("Fresh note", 2, draft.Revision); got != 204 {
		t.Fatalf("new draft after finalize: %d", got)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notes").Scan(&count); err != nil || count != 1 {
		t.Fatalf("finalization count=%d err=%v", count, err)
	}
}

func TestRevisionsSurviveRestartAndMaintenanceWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revision.db")
	db.InitDB(path)
	t.Cleanup(func() { db.DB.Close() })
	login := registerTestUser(t, "rick", "a long test password")
	createTestNote(t, login, "Original")
	mustRecoveryExec(t, "UPDATE notes SET content='maintenance edit' WHERE id=1")
	db.DB.Close()
	db.InitDB(path)
	response := authenticatedRequest(t, protect(getNoteHandler, false), "GET", "/api/notes/get?id=1", "", login, false)
	var note models.Note
	if err := json.Unmarshal(response.Body.Bytes(), &note); err != nil || note.Revision != 1 || note.Content != "maintenance edit" {
		t.Fatalf("revision not retained: %+v %v", note, err)
	}
}
