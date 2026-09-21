package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gnotes/internal/db"
)

func TestAdminOverviewReportsUserActivityWithoutNoteContents(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "admin-overview.db"))
	t.Cleanup(func() { db.DB.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	adminID := insertPageTestUser(t, "rick")
	userID := insertPageTestUser(t, "alice")
	if _, err := db.DB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET last_login_at = ? WHERE id = ?", now.Add(-time.Hour), userID); err != nil {
		t.Fatal(err)
	}
	for _, deletedAt := range []any{nil, now.Add(-time.Minute)} {
		if _, err := db.DB.Exec(
			"INSERT INTO notes (user_id, title, content, created_at, deleted_at) VALUES (?, 'private title', 'private content', ?, ?)",
			userID, now.Add(-2*time.Hour), deletedAt,
		); err != nil {
			t.Fatal(err)
		}
	}
	for index, expiresAt := range []time.Time{now.Add(time.Hour), now.Add(-time.Hour)} {
		if _, err := db.DB.Exec(
			"INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
			fmt.Sprintf("token-%d", index), userID, fmt.Sprintf("csrf-%d", index), now.Add(-30*time.Minute), expiresAt,
		); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	request = request.WithContext(context.WithValue(
		request.Context(), authContextKey{}, authSession{UserID: adminID, Username: "rick", Role: "admin"},
	))
	response := httptest.NewRecorder()
	adminOverviewHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); strings.Contains(body, "private title") || strings.Contains(body, "private content") {
		t.Fatalf("overview exposed note contents: %s", body)
	}
	var overview struct {
		Users []adminUser `json:"users"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	var alice *adminUser
	for index := range overview.Users {
		if overview.Users[index].ID == userID {
			alice = &overview.Users[index]
		}
	}
	if alice == nil {
		t.Fatal("alice missing from overview")
	}
	if alice.NoteCount != 2 || alice.ActiveNotes != 1 || alice.RecycledNotes != 1 {
		t.Fatalf("note counts = total %d active %d recycled %d", alice.NoteCount, alice.ActiveNotes, alice.RecycledNotes)
	}
	if alice.LastLoginAt == nil || len(alice.Sessions) != 1 {
		t.Fatalf("activity details = last login %v sessions %+v", alice.LastLoginAt, alice.Sessions)
	}
}

func TestAdminDeleteUserRemovesAllPrivateData(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "admin-delete.db"))
	t.Cleanup(func() { db.DB.Close() })
	adminID := insertPageTestUser(t, "rick")
	userID := insertPageTestUser(t, "doomed")
	if _, err := db.DB.Exec("UPDATE users SET role = 'admin' WHERE id = ?", adminID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO notes (user_id, title, content, created_at) VALUES (?, 'note', 'body', ?)", []any{userID, now}},
		{"INSERT INTO drafts (user_id, title, content, version, updated_at) VALUES (?, 'draft', 'body', 1, ?)", []any{userID, now}},
		{"INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at) VALUES ('token', ?, 'csrf', ?, ?)", []any{userID, now, now.Add(time.Hour)}},
	}
	for _, statement := range statements {
		if _, err := db.DB.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/users/delete?id=%d", userID), nil)
	request = request.WithContext(context.WithValue(
		request.Context(), authContextKey{}, authSession{UserID: adminID, Username: "rick", Role: "admin"},
	))
	response := httptest.NewRecorder()
	adminDeleteUserHandler(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	for _, check := range []struct {
		table string
		query string
	}{
		{"users", "SELECT COUNT(*) FROM users WHERE id = ?"},
		{"notes", "SELECT COUNT(*) FROM notes WHERE user_id = ?"},
		{"drafts", "SELECT COUNT(*) FROM drafts WHERE user_id = ?"},
		{"sessions", "SELECT COUNT(*) FROM sessions WHERE user_id = ?"},
	} {
		var count int
		if err := db.DB.QueryRow(check.query, userID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s rows remaining = %d", check.table, count)
		}
	}
}

func TestFinishAuthenticationRecordsLastLogin(t *testing.T) {
	db.InitDB(filepath.Join(t.TempDir(), "last-login.db"))
	t.Cleanup(func() { db.DB.Close() })
	userID := insertPageTestUser(t, "login-user")
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	response := httptest.NewRecorder()
	finishAuthentication(response, request, userID, "login-user", "user")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var lastLogin time.Time
	if err := db.DB.QueryRow("SELECT last_login_at FROM users WHERE id = ?", userID).Scan(&lastLogin); err != nil {
		t.Fatal(err)
	}
	if lastLogin.IsZero() || time.Since(lastLogin) > time.Minute {
		t.Fatalf("last login = %v", lastLogin)
	}
}
