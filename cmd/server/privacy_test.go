package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gnotes/internal/db"
)

func privacyPOST(t *testing.T, login testLogin, handler http.HandlerFunc, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := authenticatedRequest(t, protect(handler, true), "POST", "/api/privacy", body, login, true)
	if w.Code != want {
		t.Fatalf("privacy request returned %d, want %d: %s", w.Code, want, w.Body.String())
	}
	return w
}

func TestPrivacyLifecycleAndRedaction(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	for id := 1; id <= 4; id++ {
		user := 1
		if id == 4 {
			user = 2
		}
		mustRecoveryExec(t, "INSERT INTO notes(id,user_id,title,content,tags,created_at) VALUES(?,?,'Visible title','ConfidentialOrchid content','[\"private-tag\"]',?)", id, user, time.Now().UTC())
	}
	mustRecoveryExec(t, "UPDATE notes SET archived_at=?,favorited=1 WHERE id=2", time.Now().UTC())
	mustRecoveryExec(t, "UPDATE notes SET deleted_at=? WHERE id=3", time.Now().UTC())
	privacyPOST(t, login, privacyNoteHandler, `{"id":1,"locked":true}`, 409)
	privacyPOST(t, login, privacyCodeHandler, `{"action":"setup","new_code":"0012","password":"original password"}`, 200)
	settings, err := loadPrivacySettings(1)
	if err != nil || string(settings.Hash) == "0012" || !passwordMatches(settings.Hash, "0012") {
		t.Fatal("privacy code not hashed or leading zero lost")
	}
	for _, id := range []int{1, 2, 3} {
		body := `{"id":` + jsonNumber(id) + `,"locked":true}`
		privacyPOST(t, login, privacyNoteHandler, body, 200)
		privacyPOST(t, login, privacyNoteHandler, body, 200) // Retry never unlocks.
	}
	privacyPOST(t, login, privacyNoteHandler, `{"id":4,"locked":true}`, 404)
	for _, test := range []struct {
		handler http.HandlerFunc
		path    string
	}{
		{listNotesHandler, "/api/notes/list"}, {pagedListNotesHandler, "/api/notes/page"}, {pagedListNotesHandler, "/api/notes/page?favorites=1"},
		{pagedListNotesHandler, "/api/notes/page?archive=1"}, {getNoteHandler, "/api/notes/get?id=1"}, {archivedNotesHandler, "/api/notes/archived"},
		{trashNotesHandler, "/api/notes/trash"}, {pagedTrashNotesHandler, "/api/notes/trash-page"},
		{searchNotesHandler, "/api/notes/search?q=ConfidentialOrchid"}, {pagedSearchNotesHandler, "/api/notes/search-page?q=ConfidentialOrchid&include_archived=1"},
		{tagsHandler, "/api/tags"}, {offlineSnapshotHandler, "/api/notes/offline"},
	} {
		w := authenticatedRequest(t, protect(test.handler, false), "GET", test.path, "", login, false)
		if w.Code != 200 || strings.Contains(w.Body.String(), "ConfidentialOrchid") || strings.Contains(w.Body.String(), "private-tag") {
			t.Fatalf("private content escaped %s: %d %s", test.path, w.Code, w.Body.String())
		}
	}
	for _, test := range []struct {
		handler    http.HandlerFunc
		path, body string
	}{
		{updateNoteHandler, "/api/notes/update?id=1", `{"title":"Overwrite","content":"stale edit","revision":0}`},
		{updateNoteTagsHandler, "/api/notes/tags?id=1", `{"tags":["changed"]}`},
		{updateNoteColorHandler, "/api/notes/color?id=1", `{"color":"sage"}`},
	} {
		w := authenticatedRequest(t, protect(test.handler, true), "PUT", test.path, test.body, login, true)
		if w.Code != 423 {
			t.Fatalf("private note edited through %s: %d %s", test.path, w.Code, w.Body.String())
		}
	}
	w := authenticatedRequest(t, protect(exportNotesHandler, false), "GET", "/api/notes/export?format=gnotes", "", login, false)
	if w.Code != 423 || strings.Contains(w.Body.String(), "ConfidentialOrchid") {
		t.Fatal("plain export bypassed privacy")
	}
	privacyPOST(t, login, privacyNoteHandler, `{"id":1,"locked":false,"code":"0013"}`, 403)
	privacyPOST(t, login, privacyNoteHandler, `{"id":1,"locked":false,"code":"0012"}`, 200)
	w = authenticatedRequest(t, protect(getNoteHandler, false), "GET", "/api/notes/get?id=1", "", login, false)
	if !strings.Contains(w.Body.String(), "ConfidentialOrchid") {
		t.Fatal("unlocked note did not return")
	}
	var count int
	db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id=1 AND privacy_locked=1").Scan(&count)
	if count != 2 {
		t.Fatal("individual unlock unlocked other notes")
	}
	privacyPOST(t, login, privacyCodeHandler, `{"action":"change","code":"0012","new_code":"4567","hide_titles":true}`, 200)
	privacyPOST(t, login, privacyNoteHandler, `{"all":true,"locked":false,"code":"0012"}`, 403)
	w = authenticatedRequest(t, protect(pagedListNotesHandler, false), "GET", "/api/notes/page?archive=1", "", login, false)
	if strings.Contains(w.Body.String(), "Visible title") {
		t.Fatal("hidden title escaped")
	}
	w = authenticatedRequest(t, protect(transferListHandler, false), "GET", "/api/notes/transfer-list", "", login, false)
	var listed []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}
	if json.Unmarshal(w.Body.Bytes(), &listed) != nil {
		t.Fatal("invalid transfer list")
	}
	for _, item := range listed {
		if (item.ID == 2 || item.ID == 3) && item.Title != "" {
			t.Fatal("transfer list leaked hidden title")
		}
	}
	privacyPOST(t, login, privacyCodeHandler, `{"action":"reset","new_code":"8888","password":"wrong password"}`, 403)
	// A forgotten/wrong code's lockout must not block account-password recovery.
	mustRecoveryExec(t, "UPDATE rate_limits SET attempts=20 WHERE scope='privacy-code'")
	w = privacyPOST(t, login, privacyNoteHandler, `{"all":true,"locked":false,"code":"4567"}`, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry delay")
	}
	privacyPOST(t, login, privacyCodeHandler, `{"action":"reset","new_code":"8888","password":"original password"}`, 200)
	db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id=1 AND privacy_locked=1").Scan(&count)
	if count != 2 {
		t.Fatal("recovery unexpectedly unlocked notes")
	}
	privacyPOST(t, login, privacyNoteHandler, `{"all":true,"locked":false,"code":"8888"}`, 200)
	db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE user_id=1 AND privacy_locked=1").Scan(&count)
	if count != 0 {
		t.Fatal("unlock all missed archive/trash")
	}
	var content string
	db.DB.QueryRow("SELECT content FROM notes WHERE id=1").Scan(&content)
	if content != "ConfidentialOrchid content" {
		t.Fatal("privacy modified note text")
	}
}

func TestPrivacyValidationAndMFARecovery(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	for _, test := range []struct {
		body string
		csrf bool
		want int
	}{
		{`{"action":"setup","new_code":"1234","password":"original password"}`, false, 403},
		{`{"action":"setup","new_code":"123","password":"original password"}`, true, 400},
		{`{"action":"setup","new_code":"１２３４","password":"original password"}`, true, 400},
		{`{"action":"setup","new_code":"1234567890123","password":"original password"}`, true, 400},
		{`{"action":"setup","new_code":"1234","password":"original password","user_id":2}`, true, 400},
	} {
		w := authenticatedRequest(t, protect(privacyCodeHandler, true), "POST", "/api/privacy/code", test.body, login, test.csrf)
		if w.Code != test.want {
			t.Fatalf("invalid setup status %d: %s", w.Code, w.Body.String())
		}
	}
	privacyPOST(t, login, privacyCodeHandler, `{"action":"setup","new_code":"1234","password":"original password"}`, 200)
	mustRecoveryExec(t, "INSERT INTO mfa_factors(user_id,secret,enabled_at) VALUES(1,'JBSWY3DPEHPK3PXP',?)", time.Now().UTC())
	privacyPOST(t, login, privacyCodeHandler, `{"action":"reset","new_code":"5678","password":"original password"}`, 403)
	mustRecoveryExec(t, "INSERT INTO mfa_recovery_codes(user_id,code_hash) VALUES(1,?)", recoveryCodeHash("01234567-89ABCDEF-01234567-89ABCDEF"))
	privacyPOST(t, login, privacyCodeHandler, `{"action":"reset","new_code":"5678","password":"original password","mfa_code":"01234567-89ABCDEF-01234567-89ABCDEF"}`, 200)
	privacyPOST(t, login, privacyCodeHandler, `{"action":"reset","new_code":"9999","password":"original password","mfa_code":"01234567-89ABCDEF-01234567-89ABCDEF"}`, 403)
	settings, _ := loadPrivacySettings(1)
	if !passwordMatches(settings.Hash, "5678") {
		t.Fatal("failed MFA recovery changed privacy code")
	}
}
