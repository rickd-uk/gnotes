package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gnotes/internal/db"
)

func TestDictionaryOwnershipAndValidation(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	mux := http.NewServeMux()
	registerDictionaryRoutes(mux)
	for _, route := range []string{"lookup?word=dictionary", "words", "export"} {
		got := httptest.NewRecorder()
		mux.ServeHTTP(got, httptest.NewRequest("GET", "/api/dictionary/"+route, nil))
		if got.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", route, got.Code)
		}
	}
	save := protect(dictionarySaveHandler, true)
	if got := authenticatedRequest(t, save, "POST", "/api/dictionary/save", `{"word":"serendipity"}`, login, false); got.Code != 403 {
		t.Fatal("saving words requires CSRF")
	}
	for _, input := range []string{`{"word":"<script>"}`, `{"word":""}`, `{"word":"123"}`, `{"word":"` + strings.Repeat("a", 97) + `"}`} {
		if got := authenticatedRequest(t, save, "POST", "/api/dictionary/save", input, login, true); got.Code != 400 {
			t.Fatalf("accepted invalid word: %s", input)
		}
	}
	for _, input := range []string{`{"word":"Serendipity"}`, `{"word":" serendipity "}`} {
		if got := authenticatedRequest(t, save, "POST", "/api/dictionary/save", input, login, true); got.Code != 200 {
			t.Fatalf("save: %d %s", got.Code, got.Body.String())
		}
	}
	var count int
	db.DB.QueryRow("SELECT count(*) FROM saved_words WHERE user_id=1").Scan(&count)
	if count != 1 {
		t.Fatal("saving twice duplicated the word")
	}
	lookup := authenticatedRequest(t, protect(dictionaryLookupHandler, false), "GET", "/api/dictionary/lookup?word=serendipity", "", login, false)
	if lookup.Code != 200 || !strings.Contains(lookup.Body.String(), `"saved":true`) || !strings.Contains(lookup.Body.String(), "good luck") {
		t.Fatalf("lookup: %s", lookup.Body.String())
	}
	other := httptest.NewRecorder()
	dictionaryWordsHandler(other, tagsTestRequest("GET", "/api/dictionary/words", nil, 2))
	if other.Code != 200 || strings.Contains(other.Body.String(), "serendipity") {
		t.Fatal("another account can see saved words")
	}
	other = httptest.NewRecorder()
	dictionaryRemoveHandler(other, tagsTestRequest("DELETE", "/api/dictionary/remove?word=serendipity", nil, 2))
	db.DB.QueryRow("SELECT count(*) FROM saved_words WHERE user_id=1").Scan(&count)
	if count != 1 {
		t.Fatal("another account removed the word")
	}
	if got := authenticatedRequest(t, protect(dictionaryRemoveHandler, true), "DELETE", "/api/dictionary/remove?word=serendipity", "", login, false); got.Code != 403 {
		t.Fatal("removing requires CSRF")
	}
	export := authenticatedRequest(t, protect(dictionaryExportHandler, false), "GET", "/api/dictionary/export", "", login, false)
	if export.Code != 200 || !strings.Contains(export.Body.String(), "serendipity") || export.Header().Get("Content-Disposition") == "" {
		t.Fatal("saved words export failed")
	}
	if got := authenticatedRequest(t, protect(dictionaryRemoveHandler, true), "DELETE", "/api/dictionary/remove?word=serendipity", "", login, true); got.Code != 204 {
		t.Fatal("cannot remove saved word")
	}
	// The same word in both accounts is independent, and user deletion cascades.
	mustRecoveryExec(t, "INSERT INTO saved_words VALUES (1,'dictionary','2026-10-06'),(2,'dictionary','2026-10-06')")
	mustRecoveryExec(t, "DELETE FROM users WHERE id=2")
	db.DB.QueryRow("SELECT count(*) FROM saved_words").Scan(&count)
	if count != 1 {
		t.Fatal("user deletion did not remove only that account's words")
	}
}

func TestDictionaryPaginationAndFiltering(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	for i := 0; i < 53; i++ {
		mustRecoveryExec(t, "INSERT INTO saved_words VALUES (1,?,?)", fmt.Sprintf("word-%02d", i), "2026-10-06")
	}
	read := func(target string) struct {
		Words      []savedWord `json:"words"`
		NextCursor string      `json:"next_cursor"`
		Total      int         `json:"total"`
	} {
		got := authenticatedRequest(t, protect(dictionaryWordsHandler, false), "GET", target, "", login, false)
		if got.Code != 200 {
			t.Fatalf("list: %d %s", got.Code, got.Body.String())
		}
		var page struct {
			Words      []savedWord `json:"words"`
			NextCursor string      `json:"next_cursor"`
			Total      int         `json:"total"`
		}
		if err := json.Unmarshal(got.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	first := read("/api/dictionary/words")
	if len(first.Words) != 50 || first.Total != 53 || first.NextCursor != "word-49" {
		t.Fatalf("first page: %+v", first)
	}
	last := read("/api/dictionary/words?cursor=" + url.QueryEscape(first.NextCursor))
	if len(last.Words) != 3 || last.NextCursor != "" || last.Words[0].Word != "word-50" {
		t.Fatalf("last page: %+v", last)
	}
	filtered := read("/api/dictionary/words?q=WORD-5")
	if filtered.Total != 3 || len(filtered.Words) != 3 {
		t.Fatal("saved word filter failed")
	}
}

func TestDictionaryWordLimit(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	mustRecoveryExec(t, `WITH RECURSIVE entries(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM entries WHERE n<10000)
		INSERT INTO saved_words SELECT 1,'word-'||n,'2026-10-06' FROM entries`)
	if got := authenticatedRequest(t, protect(dictionarySaveHandler, true), "POST", "/api/dictionary/save", `{"word":"serendipity"}`, login, true); got.Code != 409 {
		t.Fatal("saved word limit was not enforced")
	}
	if got := authenticatedRequest(t, protect(dictionarySaveHandler, true), "POST", "/api/dictionary/save", `{"word":"word-1"}`, login, true); got.Code != 200 {
		t.Fatal("an existing word should remain idempotent at the limit")
	}
}
