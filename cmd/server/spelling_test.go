package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gnotes/internal/db"
)

func TestSpellingListsOwnershipAndValidation(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	mux := http.NewServeMux()
	registerSpellingRoutes(mux)
	for _, path := range []string{"/api/spelling", "/api/spelling/migrate"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("POST", path, nil))
		if response.Code != 401 {
			t.Fatalf("auth %d", response.Code)
		}
		response = authenticatedRequest(t, mux.ServeHTTP, "POST", path, `{}`, login, false)
		if response.Code != 403 {
			t.Fatalf("csrf %d", response.Code)
		}
	}
	read := func(user int) spellingLists {
		t.Helper()
		w := httptest.NewRecorder()
		spellingHandler(w, tagsTestRequest("GET", "/api/spelling", nil, user))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("read %d", w.Code)
		}
		var lists spellingLists
		if err := json.Unmarshal(w.Body.Bytes(), &lists); err != nil {
			t.Fatal(err)
		}
		return lists
	}
	post := func(body string, intended int) {
		t.Helper()
		w := authenticatedRequest(t, mux.ServeHTTP, "POST", "/api/spelling", body, login, true)
		if w.Code != intended {
			t.Fatalf("post %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{"scope":"word","value":"Serendipity"}`, `{"scope":"word","value":" serendipity "}`, `{"scope":"name","value":"New York","kind":"place"}`, `{"scope":"name","value":"new york","kind":"person"}`} {
		post(body, 200)
	}
	lists := read(1)
	if len(lists.Words) != 1 || lists.Words[0] != "serendipity" || len(lists.Names) != 1 || lists.Names[0].Name != "New York" || lists.Names[0].Kind != "place" {
		t.Fatalf("normalization: %+v", lists)
	}
	if other := read(2); len(other.Names)+len(other.Words) != 0 {
		t.Fatal("account leak")
	}
	w := httptest.NewRecorder()
	spellingHandler(w, tagsTestRequest("DELETE", "/api/spelling?scope=name&value=New%20York", nil, 2))
	if len(read(1).Names) != 1 {
		t.Fatal("foreign deletion")
	}
	for _, body := range []string{`{"scope":"unknown","value":"word"}`, `{"scope":"word","value":"two words"}`, `{"scope":"name","value":"<script>","kind":"person"}`, `{"scope":"name","value":"Rick","kind":"invalid"}`, `{"scope":"name","value":"123","kind":"person"}`, `{"scope":"word","value":"` + strings.Repeat("x", 97) + `"}`, `{"scope":"word","value":"test","user_id":2}`} {
		post(body, 400)
	}
	if response := authenticatedRequest(t, mux.ServeHTTP, "DELETE", "/api/spelling?scope=word&value=serendipity", "", login, false); response.Code != 403 {
		t.Fatal("delete without csrf")
	}
	if response := authenticatedRequest(t, mux.ServeHTTP, "DELETE", "/api/spelling?scope=word&value=SERENDIPITY", "", login, true); response.Code != 200 || len(read(1).Words) != 0 {
		t.Fatal("delete word")
	}
	mustRecoveryExec(t, "INSERT INTO spelling_entries VALUES(2,'name','other','Other','person','2026-10-06')")
	mustRecoveryExec(t, "DELETE FROM users WHERE id=2")
	var count int
	db.DB.QueryRow("SELECT COUNT(*) FROM spelling_entries").Scan(&count)
	if count != 1 {
		t.Fatal("account deletion must cascade")
	}
}

func TestSpellingMigrationAtomicMergeAndLimit(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	mux := http.NewServeMux()
	registerSpellingRoutes(mux)
	mustRecoveryExec(t, "INSERT INTO spelling_entries VALUES(1,'name','new york','New York','place','2026-10-06')")
	migrate := func(body string, want int) {
		t.Helper()
		w := authenticatedRequest(t, mux.ServeHTTP, "POST", "/api/spelling/migrate", body, login, true)
		if w.Code != want {
			t.Fatalf("migrate %d %s", w.Code, w.Body.String())
		}
	}
	body := `{"words":["Example","example"],"names":[{"name":"new york","kind":"company"},{"name":"Sriniously","kind":"person"}]}`
	migrate(body, 200)
	migrate(body, 200)
	var count int
	db.DB.QueryRow("SELECT COUNT(*) FROM spelling_entries WHERE user_id=1").Scan(&count)
	if count != 3 {
		t.Fatalf("merge duplicated: %d", count)
	}
	migrate(`{"words":["valid"],"names":[{"name":"Invalid","kind":"bad"}]}`, 400)
	db.DB.QueryRow("SELECT COUNT(*) FROM spelling_entries WHERE user_id=1").Scan(&count)
	if count != 3 {
		t.Fatal("invalid migration partially saved")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1999; i++ {
		if _, err := tx.Exec("INSERT INTO spelling_entries VALUES(1,'word',?,?,'word','2026-10-06')", fmt.Sprintf("word%d", i), fmt.Sprintf("word%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	migrate(`{"words":["overflow"],"names":[{"name":"Extra","kind":"other"}]}`, 409)
	db.DB.QueryRow("SELECT COUNT(*) FROM spelling_entries WHERE scope='name'").Scan(&count)
	if count != 2 {
		t.Fatal("limit failure partially saved")
	}
	if w := authenticatedRequest(t, mux.ServeHTTP, "POST", "/api/spelling", `{"scope":"word","value":"example"}`, login, true); w.Code != 200 {
		t.Fatal("existing entry must work at limit")
	}
	if w := authenticatedRequest(t, mux.ServeHTTP, "POST", "/api/spelling", `{"scope":"word","value":"overflow"}`, login, true); w.Code != 409 {
		t.Fatal("limit not enforced")
	}
	mustRecoveryExec(t, "DELETE FROM spelling_entries WHERE user_id=1 AND scope='word' AND entry_key='word0'")
	migrate(`{"words":["partial","overflow"],"names":[]}`, 409)
	db.DB.QueryRow("SELECT COUNT(*) FROM spelling_entries WHERE entry_key='partial'").Scan(&count)
	if count != 0 {
		t.Fatal("migration did not roll back partial additions")
	}
}
