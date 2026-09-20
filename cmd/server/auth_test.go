package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestLegacySchemaMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE notes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT,
			content TEXT,
			created_at DATETIME
		);
		INSERT INTO notes (title, content, created_at) VALUES ('kept', 'legacy note', CURRENT_TIMESTAMP);`)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	db.InitDB(path)
	t.Cleanup(func() { db.DB.Close() })
	var noteCount int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notes WHERE title = 'kept' AND user_id IS NULL").Scan(&noteCount); err != nil {
		t.Fatal(err)
	}
	if noteCount != 1 {
		t.Fatalf("legacy note count = %d, want 1", noteCount)
	}
	var signups string
	if err := db.DB.QueryRow("SELECT value FROM settings WHERE key = 'signups_enabled'").Scan(&signups); err != nil {
		t.Fatal(err)
	}
	if signups != "true" {
		t.Fatalf("signups setting = %q, want true", signups)
	}
}

type testLogin struct {
	cookie *http.Cookie
	csrf   string
	role   string
}

func TestRemoteFirstAdminRequiresSetupToken(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "remote-setup.db"))
	t.Cleanup(func() { db.DB.Close() })
	t.Setenv("GNOTES_SETUP_TOKEN", "one-time-secret")

	register := func(setupToken string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(credentials{
			Username:   "rick",
			Password:   "correct horse battery staple",
			SetupToken: setupToken,
		})
		request := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
		request.RemoteAddr = "203.0.113.20:12345"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		registerHandler(response, request)
		return response
	}

	if response := register(""); response.Code != http.StatusForbidden {
		t.Fatalf("remote setup without token status = %d, want 403", response.Code)
	}
	if response := register("one-time-secret"); response.Code != http.StatusOK {
		t.Fatalf("remote setup with token status = %d, want 200: %s", response.Code, response.Body.String())
	}
}

func TestAuthenticationOwnershipCSRFAndAdministration(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "gnotes-test.db"))
	t.Cleanup(func() { db.DB.Close() })

	legacyResult, err := db.DB.Exec(
		"INSERT INTO notes (title, content, created_at) VALUES (?, ?, ?)",
		"legacy", "before accounts", time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	legacyID64, _ := legacyResult.LastInsertId()
	legacyID := int(legacyID64)

	rick := registerTestUser(t, "rick", "correct horse battery staple")
	if rick.role != "admin" {
		t.Fatalf("first rick account role = %q, want admin", rick.role)
	}
	alice := registerTestUser(t, "alice", "another long test password")
	if alice.role != "user" {
		t.Fatalf("alice role = %q, want user", alice.role)
	}

	createTestNote(t, rick, "Rick private")
	createTestNote(t, alice, "Alice private")

	rickNotes := listTestNotes(t, rick)
	if !strings.Contains(rickNotes, "Rick private") || !strings.Contains(rickNotes, "legacy") || strings.Contains(rickNotes, "Alice private") {
		t.Fatalf("rick received wrong notes: %s", rickNotes)
	}
	aliceNotes := listTestNotes(t, alice)
	if !strings.Contains(aliceNotes, "Alice private") || strings.Contains(aliceNotes, "Rick private") || strings.Contains(aliceNotes, "legacy") {
		t.Fatalf("alice received wrong notes: %s", aliceNotes)
	}

	updateBody := `{"title":"stolen","content":"no"}`
	response := authenticatedRequest(
		t, protect(updateNoteHandler, true), http.MethodPut,
		"/api/notes/update?id="+jsonNumber(legacyID), updateBody, alice, true,
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-user update status = %d, want 404", response.Code)
	}

	response = authenticatedRequest(
		t, protect(createNoteHandler, true), http.MethodPost,
		"/api/notes/create", `{"title":"missing csrf"}`, alice, false,
	)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", response.Code)
	}

	response = authenticatedRequest(
		t, protect(requireAdmin(adminOverviewHandler), false), http.MethodGet,
		"/api/admin/overview", "", alice, false,
	)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin overview status = %d, want 403", response.Code)
	}

	response = authenticatedRequest(
		t, protect(requireAdmin(adminSignupsHandler), true), http.MethodPut,
		"/api/admin/signups", `{"enabled":false}`, rick, true,
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("disable signups status = %d, want 204: %s", response.Code, response.Body.String())
	}

	blockedRegistration := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(
		`{"username":"bobby","password":"a sufficiently long password"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	registerHandler(blockedRegistration, request)
	if blockedRegistration.Code != http.StatusForbidden {
		t.Fatalf("registration while disabled status = %d, want 403", blockedRegistration.Code)
	}
}

func registerTestUser(t *testing.T, username, password string) testLogin {
	t.Helper()
	body, _ := json.Marshal(credentials{Username: username, Password: password})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	registerHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("register %s status = %d: %s", username, response.Code, response.Body.String())
	}
	var result struct {
		CSRF string `json:"csrf_token"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return testLogin{cookie: cookie, csrf: result.CSRF, role: result.Role}
		}
	}
	t.Fatal("registration did not set a session cookie")
	return testLogin{}
}

func createTestNote(t *testing.T, login testLogin, title string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": title})
	response := authenticatedRequest(
		t, protect(createNoteHandler, true), http.MethodPost,
		"/api/notes/create", string(body), login, true,
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("create note status = %d: %s", response.Code, response.Body.String())
	}
}

func listTestNotes(t *testing.T, login testLogin) string {
	t.Helper()
	response := authenticatedRequest(
		t, protect(listNotesHandler, false), http.MethodGet,
		"/api/notes/list", "", login, false,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("list notes status = %d: %s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func authenticatedRequest(
	t *testing.T,
	handler http.HandlerFunc,
	method, target, body string,
	login testLogin,
	includeCSRF bool,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.AddCookie(login.cookie)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if includeCSRF {
		request.Header.Set("X-CSRF-Token", login.csrf)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func jsonNumber(value int) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
