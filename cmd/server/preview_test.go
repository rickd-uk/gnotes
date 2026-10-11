package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gnotes/internal/db"
)

func TestNotePreview(t *testing.T) {
	_, _, login, _ := recoveryFixture(t)
	handler := protect(previewNoteHandler, true)
	unauthenticated := httptest.NewRecorder()
	handler(unauthenticated, httptest.NewRequest(http.MethodPost, "/api/notes/preview", strings.NewReader(`{"content":"private"}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated preview = %d", unauthenticated.Code)
	}
	for _, test := range []struct {
		name, method, body string
		csrf               bool
		status             int
	}{
		{"missing CSRF", "POST", `{"content":"private"}`, false, http.StatusForbidden},
		{"wrong method", "GET", "", true, http.StatusMethodNotAllowed},
		{"invalid JSON", "POST", `{`, true, http.StatusBadRequest},
		{"oversized", "POST", `{"content":"` + strings.Repeat("x", maxNoteBodyBytes+1) + `"}`, true, http.StatusBadRequest},
		{"render", "POST", `{"content":"## Preview\n\n**bold**\n\n<script>alert(1)</script>\n\n[bad](javascript:alert(2))"}`, true, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := authenticatedRequest(t, handler, test.method, "/api/notes/preview", test.body, login, test.csrf)
			if response.Code != test.status {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			if test.status == http.StatusOK {
				var result map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				html := result["html_content"]
				if !strings.Contains(html, "<h2>Preview</h2>") || !strings.Contains(html, "<strong>bold</strong>") || strings.Contains(html, "<script") || strings.Contains(html, "javascript:") {
					t.Fatalf("unsafe or incorrect preview: %s", html)
				}
				if response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("preview can be cached")
				}
			}
		})
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM notes").Scan(&count); err != nil || count != 0 {
		t.Fatalf("preview wrote notes: count=%d err=%v", count, err)
	}
}
