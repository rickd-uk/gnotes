package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOfflineSnapshotOwnershipAndReadOnly(t *testing.T) {
	recoveryFixture(t)
	other := insertPageTestUser(t, "offline-other")
	for _, entry := range []struct {
		title             string
		owner             int
		archived, deleted any
	}{
		{"Active snapshot", 1, nil, nil}, {"Archived snapshot", 1, time.Now(), nil},
		{"Recycled secret", 1, nil, time.Now()}, {"Other account secret", other, nil, nil},
	} {
		mustRecoveryExec(t, "INSERT INTO notes(user_id,title,content,created_at,archived_at,deleted_at) VALUES(?,?,'Body',?,?,?)", entry.owner, entry.title, time.Now(), entry.archived, entry.deleted)
	}
	response := httptest.NewRecorder()
	offlineSnapshotHandler(response, transferRequest(http.MethodGet, "/api/notes/offline", nil, 1))
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("snapshot response: %d %s", response.Code, response.Body.String())
	}
	var snapshot struct {
		Owner    int            `json:"owner"`
		Username string         `json:"username"`
		Notes    []transferNote `json:"notes"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Owner != 1 || snapshot.Username != "rick" || len(snapshot.Notes) != 2 {
		t.Fatalf("incorrect owner/notes: %+v", snapshot)
	}
	for _, note := range snapshot.Notes {
		if note.DeletedAt != nil || note.Title == "Other account secret" {
			t.Fatal("snapshot leaked excluded notes")
		}
	}
	response = httptest.NewRecorder()
	offlineSnapshotHandler(response, transferRequest(http.MethodGet, "/api/notes/offline", nil, other))
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Owner != other || len(snapshot.Notes) != 1 || snapshot.Notes[0].Title != "Other account secret" {
		t.Fatal("snapshot crossed accounts")
	}
	response = httptest.NewRecorder()
	offlineSnapshotHandler(response, transferRequest(http.MethodPost, "/api/notes/offline", nil, 1))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatal("snapshot accepted a write")
	}
	response = httptest.NewRecorder()
	protect(offlineSnapshotHandler, false)(response, httptest.NewRequest(http.MethodGet, "/api/notes/offline", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("snapshot accessible without a session")
	}
}
