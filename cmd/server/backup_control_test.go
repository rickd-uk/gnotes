package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupControlAuthorizationAndProxy(t *testing.T) {
	_, _, admin, hash := recoveryFixture(t)
	other := backupOtherLogin(t, hash)
	socket := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan map[string]any, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/action" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			received <- body
			w.WriteHeader(202)
			w.Write([]byte(`{"message":"accepted"}`))
		} else {
			w.Write([]byte(`{"configured":false}`))
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	t.Setenv("GNOTES_BACKUP_CONTROL_SOCKET", socket)
	action := protect(requireAdmin(backupControlActionHandler), true)
	status := protect(requireAdmin(backupControlStatusHandler), false)
	request := func(login testLogin, body string, csrf bool) *httptest.ResponseRecorder {
		return authenticatedRequest(t, action, "POST", "/api/admin/backups/action", body, login, csrf)
	}
	body := `{"action":"configure","password":"original password","region":"ap-northeast-2","bucket":"fixture-bucket","prefix":"gnotes","access_key":"fixture-access-key","secret_key":"fixture-secret-key"}`
	if w := request(other, body, true); w.Code != 403 {
		t.Fatalf("regular user: %d", w.Code)
	}
	if w := request(admin, body, false); w.Code != 403 {
		t.Fatalf("CSRF: %d", w.Code)
	}
	if w := request(admin, strings.Replace(body, "original password", "wrong", 1), true); w.Code != 403 {
		t.Fatalf("password: %d", w.Code)
	}
	if w := request(admin, `{"action":"delete","password":"original password"}`, true); w.Code != 400 {
		t.Fatalf("unknown action: %d", w.Code)
	}
	if w := authenticatedRequest(t, status, "GET", "/api/admin/backups/status", "", other, false); w.Code != 403 {
		t.Fatalf("status role: %d", w.Code)
	}
	w := request(admin, body, true)
	if w.Code != 202 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
	forwarded := <-received
	if _, ok := forwarded["password"]; ok {
		t.Fatal("login password forwarded to backup bridge")
	}
	if forwarded["secret_key"] != "fixture-secret-key" || forwarded["action"] != "configure" {
		t.Fatal("lost configuration fields")
	}
	w = authenticatedRequest(t, status, "GET", "/api/admin/backups/status", "", admin, false)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status: %d", w.Code)
	}
	t.Setenv("GNOTES_BACKUP_CONTROL_SOCKET", "")
	w = authenticatedRequest(t, status, "GET", "/api/admin/backups/status", "", admin, false)
	if w.Code != 503 {
		t.Fatalf("unconfigured: %d", w.Code)
	}
}
